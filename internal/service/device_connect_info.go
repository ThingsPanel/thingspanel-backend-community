package service

import (
	"net"
	"strconv"
	"strings"

	"project/pkg/global"
)

// A listener such as :502 is not a device-reachable host. Keep the configured
// port visible, but never invent a host from the web or internal plugin address.
func protocolConnectionInfo(address, lang string) map[string]string {
	message := global.ResponseHandler.ErrManager.GetMessage
	address = strings.TrimSpace(address)
	info := map[string]string{message(500001, lang): address}
	host, port, err := net.SplitHostPort(address)
	missingHost := address == ""
	if err == nil && (host == "" || net.ParseIP(host).IsUnspecified()) {
		missingHost = true
		if number, parseErr := strconv.Atoi(port); parseErr == nil && number > 0 && number <= 65535 {
			info[message(500012, lang)] = port
		}
	}
	if missingHost {
		info[message(500001, lang)] = message(500013, lang)
		info[message(500010, lang)] = message(500014, lang)
	}
	return info
}
