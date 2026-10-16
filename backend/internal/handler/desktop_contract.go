package handler

const DesktopContractVersion = 1

type DesktopCapabilities struct {
	LocalAssets   bool `json:"localAssets"`
	ProviderCalls bool `json:"providerCalls"`
}

type DesktopContract struct {
	Version      int                 `json:"contractVersion"`
	Profile      string              `json:"profile"`
	Capabilities DesktopCapabilities `json:"capabilities"`
}

func LocalDesktopContract() DesktopContract {
	return DesktopContract{
		Version: DesktopContractVersion,
		Profile: "local",
		Capabilities: DesktopCapabilities{
			LocalAssets:   true,
			ProviderCalls: true,
		},
	}
}

// HostedDesktopContract describes the OrcaTV surface. It keeps the same
// capability shape as the local contract so existing clients can boot, but the
// profile is explicit: platform model configuration is server-owned and
// per-user business data is scoped by the identity middleware.
func HostedDesktopContract() DesktopContract {
	return DesktopContract{
		Version: DesktopContractVersion,
		Profile: "hosted",
		Capabilities: DesktopCapabilities{
			LocalAssets:   true,
			ProviderCalls: true,
		},
	}
}
