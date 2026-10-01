package providerquota

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Layout struct{ Root string }

func LayoutAt(stateRoot string) Layout {
	return Layout{Root: filepath.Join(stateRoot, "task-board", "provider-quota")}
}
func DefaultLayout() (Layout, error) {
	root := os.Getenv("XDG_STATE_HOME")
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return Layout{}, Refuse("state_root_unavailable")
		}
	}
	return LayoutAt(root), nil
}
func (l Layout) RecordFile(key string) string { return filepath.Join(l.Root, key+".quota.json") }
func (l Layout) LockFile(key string) string   { return filepath.Join(l.Root, key+".quota.lock") }

// Options contains instance-local seams. No tests alter global IO or clocks.
type Options struct {
	Now        func() time.Time
	OwnerAlive func(int) bool
}
type Store struct {
	layout     Layout
	now        func() time.Time
	ownerAlive func(int) bool
}

// NewStore requires a caller-owned clock; it never selects an ambient clock.
func NewStore(layout Layout, options Options) (*Store, error) {
	if options.Now == nil {
		return nil, Refuse("clock_required")
	}
	if options.OwnerAlive == nil {
		options.OwnerAlive = processAlive
	}
	return &Store{layout, options.Now, options.OwnerAlive}, nil
}

var ErrAbsent = errors.New("providerquota: record absent")
var ErrLocked = errors.New("providerquota: lock held; retry")

// Read is cached IO only; it never refreshes or starts a process. All fields
// used to verify ownership come from caller evidence, not the stored record.
func (s *Store) Read(c ParseContext) (QuotaRecord, error) {
	key, err := c.Key()
	if err != nil {
		return Failure(c, "", Reason(err))
	}
	b, err := os.ReadFile(s.layout.RecordFile(key))
	if os.IsNotExist(err) {
		return QuotaRecord{}, ErrAbsent
	}
	if err != nil {
		return Failure(c, "", "store_unreadable")
	}
	var r QuotaRecord
	if json.Unmarshal(b, &r) != nil {
		return Failure(c, "", "record_invalid")
	}
	if err = Validate(r, c, s.now()); err != nil {
		return Failure(c, "", Reason(err))
	}
	return r, nil
}

// Write validates before acquiring the lock; callers retry ErrLocked rather
// than overlapping writes. The lock is independent of providerlimits leases.
func (s *Store) Write(c ParseContext, r QuotaRecord) error {
	if err := Validate(r, c, s.now()); err != nil {
		return err
	}
	lock, err := s.Acquire(context.Background(), r.Key)
	if err != nil {
		return err
	}
	defer lock.Release()
	if lock.BrokeDeadOwner {
		r.Failures = appendFailures(r.Failures, ReadFailure{Reason: "dead_owner_lock", At: s.now()})
	}
	return writeJSONAtomic(s.layout.RecordFile(r.Key), r)
}

// Merge performs a read/merge/write under one lock. A consumer that already
// holds the lock around a live read uses Read and Commit instead of reacquiring.
func (s *Store) Merge(c ParseContext, in QuotaRecord, kind UpdateKind) (QuotaRecord, error) {
	key, err := c.Key()
	if err != nil {
		return QuotaRecord{}, err
	}
	lock, err := s.Acquire(context.Background(), key)
	if err != nil {
		return QuotaRecord{}, err
	}
	defer lock.Release()
	old, err := s.Read(c)
	if err != nil && !errors.Is(err, ErrAbsent) {
		return old, err
	}
	if errors.Is(err, ErrAbsent) {
		old, err = Base(c, in.Source)
		if err != nil {
			return QuotaRecord{}, err
		}
		old.State = Exact
		old.WindowsDigest, _ = WindowsDigest(old.Windows)
	}
	merged, err := MergeRecord(old, in, kind, c)
	if err != nil {
		return merged, err
	}
	if lock.BrokeDeadOwner {
		merged.Failures = appendFailures(merged.Failures, ReadFailure{Reason: "dead_owner_lock", At: s.now()})
	}
	if err = s.Commit(lock, c, merged); err != nil {
		return QuotaRecord{}, err
	}
	return merged, nil
}

// Commit writes while the supplied live lock belongs to this store and key.
func (s *Store) Commit(lock *Lock, c ParseContext, r QuotaRecord) error {
	if lock == nil || lock.store != s || lock.key != r.Key || lock.released {
		return Refuse("lock_required")
	}
	if err := Validate(r, c, s.now()); err != nil {
		return err
	}
	info, err := os.Stat(lock.path)
	if err != nil || !os.SameFile(info, lock.info) {
		return Refuse("lock_lost")
	}
	return writeJSONAtomic(s.layout.RecordFile(r.Key), r)
}

type UpdateKind string

const (
	Pull   UpdateKind = "pull"
	Push   UpdateKind = "push"
	Failed UpdateKind = "failed"
)

func appendFailures(old []ReadFailure, more ...ReadFailure) []ReadFailure {
	out := append(append([]ReadFailure{}, old...), more...)
	if len(out) > MaxFailures {
		out = out[len(out)-MaxFailures:]
	}
	return out
}

// MergeRecord is pure. Successful pulls replace all windows; sparse pushes
// replace matching (id, kind, scope) windows only. Failures never change any
// window, its time, record observed_at, source, or digest.
func MergeRecord(old, in QuotaRecord, kind UpdateKind, c ParseContext) (QuotaRecord, error) {
	if err := Validate(old, c, c.RetrievedAt); err != nil {
		return QuotaRecord{}, err
	}
	if err := Validate(in, c, c.RetrievedAt); err != nil {
		return QuotaRecord{}, err
	}
	// Deep copy prevents a caller from mutating cached windows through pointers.
	b, _ := json.Marshal(old)
	var out QuotaRecord
	if err := json.Unmarshal(b, &out); err != nil {
		return QuotaRecord{}, Refuse("record_invalid")
	}
	switch kind {
	case Failed:
		if len(in.Failures) == 0 {
			return QuotaRecord{}, Refuse("failure_required")
		}
		out.Failures = appendFailures(out.Failures, in.Failures...)
		out.RetrievedAt = in.RetrievedAt
		out.State = Unavailable
		return out, nil
	case Pull, Push:
		if in.State == Unavailable || in.State == NotSupported {
			return QuotaRecord{}, Refuse("update_invalid")
		}
		if kind == Pull {
			out = in
		} else {
			ws := canonicalWindows(out.Windows)
			for _, w := range in.Windows {
				replaced := false
				for i := range ws {
					if windowKey(ws[i]) == windowKey(w) {
						ws[i] = w
						replaced = true
						break
					}
				}
				if !replaced {
					ws = append(ws, w)
				}
			}
			out = in
			out.Windows = ws
			out.Source = "session-push"
		}
		out.Failures = appendFailures(old.Failures, in.Failures...)
		return Finish(out, c)
	default:
		return QuotaRecord{}, Refuse("update_invalid")
	}
}
func writeJSONAtomic(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return Refuse("record_invalid")
	}
	data = append(data, '\n')
	if os.MkdirAll(filepath.Dir(path), 0o700) != nil {
		return Refuse("store_unwritable")
	}
	file, err := os.CreateTemp(filepath.Dir(path), "quota-*.tmp")
	if err != nil {
		return Refuse("store_unwritable")
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		_ = file.Close()
		return Refuse("store_unwritable")
	}
	if err = file.Sync(); err != nil {
		_ = file.Close()
		return Refuse("store_unwritable")
	}
	if file.Close() != nil {
		return Refuse("store_unwritable")
	}
	if os.Rename(file.Name(), path) != nil {
		return Refuse("store_unwritable")
	}
	return nil
}

const DeadLockAge = 30 * time.Second

// lockOwner keeps acquired_at as a string: decoding it into time.Time would
// consult ambient Local for a numeric offset and could read a TZ-selected file.
// It is written in UTC and parsed only through ParseTimestamp.
type lockOwner struct {
	PID        int     `json:"pid"`
	AcquiredAt *string `json:"acquired_at"`
}
type Lock struct {
	store          *Store
	key, path      string
	info           os.FileInfo
	released       bool
	BrokeDeadOwner bool
}

// Acquire is nonblocking. O_EXCL selects one winner; a live owner always
// wins, even when old. Only readable, positive, dead PIDs older than
// DeadLockAge can be broken. Corrupt/young locks are retained conservatively.
func (s *Store) Acquire(ctx context.Context, key string) (*Lock, error) {
	if !validKey(key) {
		return nil, Refuse("key_invalid")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if os.MkdirAll(s.layout.Root, 0o700) != nil {
		return nil, Refuse("store_unwritable")
	}
	path := s.layout.LockFile(key)
	broken := false
	for attempt := 0; attempt < 2; attempt++ {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			info, statErr := file.Stat()
			acquired := s.now().UTC().Format(time.RFC3339Nano)
			payload, _ := json.Marshal(lockOwner{os.Getpid(), &acquired})
			_, writeErr := file.Write(payload)
			closeErr := file.Close()
			if statErr != nil || writeErr != nil || closeErr != nil {
				_ = os.Remove(path)
				return nil, Refuse("lock_unwritable")
			}
			return &Lock{store: s, key: key, path: path, info: info, BrokeDeadOwner: broken}, nil
		}
		if !os.IsExist(err) {
			return nil, Refuse("lock_unwritable")
		}
		if attempt > 0 {
			break
		}
		info, err := os.Stat(path)
		if err != nil || s.now().Sub(info.ModTime()) < DeadLockAge {
			break
		}
		payload, err := os.ReadFile(path)
		var owner lockOwner
		if err != nil || json.Unmarshal(payload, &owner) != nil || owner.PID <= 0 || !validLockTime(owner.AcquiredAt) || s.ownerAlive(owner.PID) {
			break
		}
		check, err := os.Stat(path)
		if err != nil || !os.SameFile(info, check) {
			break
		}
		if os.Remove(path) != nil {
			break
		}
		broken = true
	}
	return nil, ErrLocked
}

// validLockTime accepts an absent or null acquisition time (as the earlier
// time.Time decoding did) or a UTC-parseable one; any other present value,
// including "", is corrupt and keeps the lock like any corrupt owner record.
func validLockTime(s *string) bool {
	_, err := ParseTimestamp(s)
	return err == nil
}
func (l *Lock) Release() error {
	if l == nil || l.released {
		return Refuse("lock_released")
	}
	l.released = true
	info, err := os.Stat(l.path)
	if err != nil || !os.SameFile(info, l.info) {
		return Refuse("lock_lost")
	}
	if os.Remove(l.path) != nil {
		return Refuse("lock_unwritable")
	}
	return nil
}
