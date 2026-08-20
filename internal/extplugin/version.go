package extplugin

import "bytes"

// Version is the plugin contract version reported to the host. Release
// versioning of the shipped artifact is handled by release-please; x:embed
// the module version here once Go supports it without VCS stamping.
const Version = "0.1.1" // x-release-please-version

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
