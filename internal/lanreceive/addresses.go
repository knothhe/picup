package lanreceive

import (
	"net"
	"sort"
	"strings"
)

// Address is a reachable IPv4 candidate. Name identifies its network interface
// so users with several networks can choose the same one as their phone.
type Address struct{ IP, Name string }

func Addresses() ([]Address, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var found []Address
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 || virtualInterface(iface.Name) {
			continue
		}
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, address := range addresses {
			ip, _, err := net.ParseCIDR(address.String())
			if err != nil || ip.To4() == nil || !(ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
				continue
			}
			found = append(found, Address{IP: ip.String(), Name: iface.Name})
		}
	}
	sort.Slice(found, func(i, j int) bool {
		ip, jp := net.ParseIP(found[i].IP), net.ParseIP(found[j].IP)
		if ip.IsLinkLocalUnicast() != jp.IsLinkLocalUnicast() {
			return !ip.IsLinkLocalUnicast()
		}
		if found[i].Name != found[j].Name {
			return found[i].Name < found[j].Name
		}
		return found[i].IP < found[j].IP
	})
	return found, nil
}

func virtualInterface(name string) bool {
	for _, prefix := range []string{"utun", "tun", "tap", "wg", "tailscale", "docker", "veth", "vmnet", "vboxnet", "awdl", "llw", "ipsec"} {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			return true
		}
	}
	return false
}
