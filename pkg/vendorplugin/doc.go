// Package vendorplugin resolves vendor models and runtime launch requests.
// BuildLaunchWithEnvironment returns the admitted Plan and sorted owned
// environment literals from the same effective request, including vendor
// translation, authoritative runtime/vendor assignment, preparation and alias
// projection. It builds the plan once; consumers must not reconstruct the
// request or diff the snapshot against a parent. BuildLaunch remains compatible.
package vendorplugin
