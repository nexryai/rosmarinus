package security

import (
	"net"
	"net/url"
	"strings"
)

func IsAllowedURL(checkURL string, allowUnsafeConnections bool) bool {
	parsedURL, err := url.Parse(checkURL)
	if err != nil {
		return false
	}

	// https以外は拒否
	// allowUnsafeConnectionsは、Noteの本文などHTTPSで保護されていないURLでも許可するべき場合のみtrueにし、基本的にはfalseで利用する
	if allowUnsafeConnections {
		if parsedURL.Scheme != "https" && parsedURL.Scheme != "http" {
			return false			
		}
	} else {
		if parsedURL.Scheme != "https" {
			return false			
		}
	}

	// UnixソケットとIPv6アドレス指定を拒否
	if strings.Contains(parsedURL.Hostname(), ":") {
		return false
	}

	// おかしいね
	if !strings.Contains(parsedURL.Hostname(), ".") {
		return false
	}

	// 認証情報を含むのは拒否
	if parsedURL.User != nil {
		return false
	}

	port := parsedURL.Port()
	if port != "" && port != "80" && port != "443" {
		// 宛先が80と443以外ならブロック
		return false
	}

	// hostnameがIPアドレスか検証
	ip := net.ParseIP(parsedURL.Hostname())
	if ip != nil {
		//IPアドレスが指定されている場合、それがプライベートアドレスならブロック
		if IsPrivateAddress(ip.String()) {
			return false
		}
	}

	return true
}
