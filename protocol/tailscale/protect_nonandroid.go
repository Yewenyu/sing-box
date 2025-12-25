//go:build !android

package tailscale

import "github.com/sagernet/sing-box/experimental/clash/platform"

func setAndroidProtectFunc(platformInterface platform.Interface) {
}
