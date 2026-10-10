package toolprobe

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/relux-works/skill-agents-management/internal/execfixture"
	"github.com/relux-works/skill-agents-management/pkg/agentic"
)

// The execution deadline cannot rescue a missing overflow notification. The
// child first announces readiness, then emits cap+1 bytes and holds both pipes
// until killed. The 30s watchdog diagnoses a hang; no sleep establishes state.
func TestProbeOverflowDoesNotWaitForExecutionDeadline(t *testing.T) {
	for _, stage := range []string{agentic.ProbeStageVersion, agentic.ProbeStageHelp} {
		t.Run(stage, func(t *testing.T) {
			gate := execfixture.NewGate(t)
			hold := execfixture.NewGate(t)
			limit, flag := maxVersionBytes, versionArg
			if stage == agentic.ProbeStageHelp {
				limit, flag = maxHelpBytes, helpArg
			}
			stub := writeProbeStub(t, gate.Command()+"\n"+
				fmt.Sprintf("printf '%%%ds' ''\n", limit+1)+hold.Command()+"\n")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				_, err := runProbe(ctx, stage, stub, []string{}, flag, limit, time.Hour)
				result <- err
			}()
			gate.Wait(t)
			gate.Release(t)
			watchdog := time.NewTimer(30 * time.Second)
			defer watchdog.Stop()
			select {
			case err := <-result:
				attempt := requireAttempt(t, err, stage, false, true)
				if attempt.TeardownIncomplete {
					t.Fatal("overflow did not reap the child and drain both pipes")
				}
			case <-watchdog.C:
				cancel()
				<-result // Keep ownership through cleanup, including on failure.
				t.Fatal("overflow waited for the execution deadline")
			}
		})
	}
}

func TestCappedBufferSignalsFirstExcessByte(t *testing.T) {
	b := newCappedBuffer(4)
	if n, err := b.Write([]byte("1234")); n != 4 || err != nil {
		t.Fatalf("exact cap write = %d, %v", n, err)
	}
	select {
	case <-b.limited:
		t.Fatal("exact cap reported overflow")
	default:
	}
	if n, err := b.Write([]byte("5")); n != 0 || err == nil {
		t.Fatalf("first excess byte = %d, %v", n, err)
	}
	select {
	case <-b.limited:
	default:
		t.Fatal("first excess byte did not signal overflow")
	}
	if n, err := b.Write([]byte("6789")); n != 0 || err == nil || string(b.Bytes()) != "1234" {
		t.Fatalf("capture grew after overflow: n=%d err=%v bytes=%q", n, err, b.Bytes())
	}
}
