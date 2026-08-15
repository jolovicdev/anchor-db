// Package version reports the running AnchorDB version.
package version

import "runtime/debug"

// Name is the tool family reported by --version and by the MCP handshake.
const Name = "anchordb"

// Version is the released version. It is the fallback for builds that carry no
// module information, such as `go build` from a working tree.
var Version = "1.0.4"

// String reports the running version.
//
// A binary installed with `go install ...@v1.2.3` records that tag in its build
// info, which is more trustworthy than the constant above: the constant is
// whatever the source said when it was written, while the build info is what
// was actually installed. Prefer it when it names a real version.
func String() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return Version
}
