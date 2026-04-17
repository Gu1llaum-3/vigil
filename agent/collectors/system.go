//go:build linux

package collectors

import (
	"bufio"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/Gu1llaum-3/vigil/internal/common"
)

// CollectSystem gathers OS, hardware, network, and identity information.
func CollectSystem() (hostname string, primaryIP string, osInfo common.OSInfo, kernel string, arch string, uptimeSecs uint64, resources common.ResourceInfo, network common.NetworkInfo, err error) {
	hostname, _ = os.Hostname()
	arch = runtime.GOARCH

	osInfo = collectOSInfo()
	kernel = collectKernel()
	uptimeSecs = collectUptime()
	resources = collectResources()
	primaryIP = collectPrimaryIP()
	network = collectNetwork()

	return hostname, primaryIP, osInfo, kernel, arch, uptimeSecs, resources, network, nil
}

// OSFamily returns the OS family (Debian or RedHat) based on /etc/os-release.
func OSFamily() string {
	return collectOSInfo().Family
}

func collectOSInfo() common.OSInfo {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return common.OSInfo{}
	}
	defer f.Close()

	fields := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if idx := strings.Index(line, "="); idx != -1 {
			key := line[:idx]
			val := strings.Trim(line[idx+1:], `"`)
			fields[key] = val
		}
	}

	family := "Unknown"
	idLike := strings.ToLower(fields["ID_LIKE"])
	id := strings.ToLower(fields["ID"])

	if strings.Contains(idLike, "debian") || id == "debian" || id == "ubuntu" || strings.Contains(idLike, "ubuntu") {
		family = "Debian"
	} else if strings.Contains(idLike, "rhel") || strings.Contains(idLike, "fedora") ||
		id == "rhel" || id == "centos" || id == "fedora" || id == "rocky" || id == "almalinux" {
		family = "RedHat"
	}

	return common.OSInfo{
		Family:  family,
		Name:    fields["NAME"],
		Version: fields["VERSION_ID"],
	}
}

func collectKernel() string {
	data, err := os.ReadFile("/proc/version")
	if err != nil {
		return ""
	}
	// Format: "Linux version 5.15.0-... (gcc ...)"
	parts := strings.Fields(string(data))
	if len(parts) >= 3 {
		return parts[2]
	}
	return strings.TrimSpace(string(data))
}

func collectUptime() uint64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	parts := strings.Fields(string(data))
	if len(parts) == 0 {
		return 0
	}
	f, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0
	}
	return uint64(f)
}

func collectResources() common.ResourceInfo {
	info := common.ResourceInfo{}

	// CPU info
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "model name") {
				if idx := strings.Index(line, ":"); idx != -1 {
					info.CPUModel = strings.TrimSpace(line[idx+1:])
				}
			}
			if strings.HasPrefix(line, "processor") {
				info.CPUCores++
			}
		}
	}

	// Memory info
	data, err = os.ReadFile("/proc/meminfo")
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			val, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				continue
			}
			switch parts[0] {
			case "MemTotal:":
				info.RAMMB = val / 1024
			case "SwapTotal:":
				info.SwapMB = val / 1024
			}
		}
	}

	return info
}

func collectPrimaryIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			return ip.String()
		}
	}
	return ""
}

func collectNetwork() common.NetworkInfo {
	return common.NetworkInfo{
		Gateway:    collectGateway(),
		DNSServers: collectDNSServers(),
	}
}

func collectGateway() string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Scan() // skip header
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		// Destination 00000000 = default route
		if fields[1] != "00000000" {
			continue
		}
		// Gateway is in hex little-endian
		gw, err := strconv.ParseUint(fields[2], 16, 32)
		if err != nil {
			continue
		}
		// Convert hex to dotted quad
		return net.IP{byte(gw), byte(gw >> 8), byte(gw >> 16), byte(gw >> 24)}.String()
	}
	return ""
}

func collectDNSServers() []string {
	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	var servers []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "nameserver ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				servers = append(servers, parts[1])
			}
		}
	}
	return servers
}
