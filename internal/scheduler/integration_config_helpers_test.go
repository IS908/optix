package scheduler

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func resolveIntegrationIBConfig() (IBConfig, error) {
	host := strings.TrimSpace(os.Getenv("OPTIX_IB_HOST"))
	if host == "" {
		host = "127.0.0.1"
	}
	rawPort := strings.TrimSpace(os.Getenv("OPTIX_IB_PORT"))
	if rawPort == "" {
		rawPort = "gateway"
	}
	port, err := resolveIntegrationIBPort(rawPort)
	if err != nil {
		return IBConfig{}, err
	}
	return IBConfig{Host: host, Port: port}, nil
}

func resolveIntegrationIBPort(raw string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "gateway":
		return 4001, nil
	case "tws":
		return 7496, nil
	default:
		port, err := strconv.Atoi(raw)
		if err != nil {
			return 0, fmt.Errorf("invalid OPTIX_IB_PORT %q: use gateway, tws, or a numeric port", raw)
		}
		return port, nil
	}
}
