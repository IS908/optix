package scheduler

import "testing"

func TestResolveIntegrationIBConfigDefaultsToGateway(t *testing.T) {
	t.Setenv("OPTIX_IB_HOST", "")
	t.Setenv("OPTIX_IB_PORT", "")

	cfg, err := resolveIntegrationIBConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 4001 {
		t.Fatalf("cfg = %+v, want default Gateway 127.0.0.1:4001", cfg)
	}
}

func TestResolveIntegrationIBConfigAcceptsAliasesAndNumericPorts(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want int
	}{
		{name: "gateway", raw: "gateway", want: 4001},
		{name: "tws", raw: "tws", want: 7496},
		{name: "numeric", raw: "4002", want: 4002},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OPTIX_IB_HOST", "192.0.2.10")
			t.Setenv("OPTIX_IB_PORT", tc.raw)

			cfg, err := resolveIntegrationIBConfig()
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Host != "192.0.2.10" || cfg.Port != tc.want {
				t.Fatalf("cfg = %+v, want host 192.0.2.10 port %d", cfg, tc.want)
			}
		})
	}
}

func TestResolveIntegrationIBConfigRejectsBadPort(t *testing.T) {
	t.Setenv("OPTIX_IB_PORT", "paper-gateway")

	if _, err := resolveIntegrationIBConfig(); err == nil {
		t.Fatal("expected invalid port error")
	}
}
