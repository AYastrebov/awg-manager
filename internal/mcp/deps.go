package mcp

import "context"

// Deps is everything the tools need from the host. Production wires
// localdeps (internal services); tests and cmd/mcp-dev wire mcptest.Fake.
// Implementations return plain errors with user-facing messages: tools
// forward err.Error() to the model verbatim.
type Deps interface {
	SystemStatus(ctx context.Context) (SystemStatus, error)

	ListTunnels(ctx context.Context) ([]TunnelSummary, error)
	GetTunnel(ctx context.Context, id string) (TunnelDetail, error)
	ControlTunnel(ctx context.Context, id, action string) error
	// ImportTunnel creates a tunnel from .conf text. warnings carries
	// address conflicts with other interfaces (non-fatal, as in the REST
	// import): the tunnel exists but may not route until the user resolves
	// them, and the model must not enable it as if all were well.
	ImportTunnel(ctx context.Context, name, config string) (created TunnelSummary, warnings []string, err error)
	// ReplaceTunnelConfig swaps a tunnel's .conf. A RUNNING tunnel is
	// stopped and started around the swap (a kernel tunnel does not pick up
	// a new Address/DNS/MTU otherwise), so the returned warnings carry both
	// a failed restart and any address conflicts the new config introduces.
	ReplaceTunnelConfig(ctx context.Context, id, config, newName string) (warnings []string, err error)
	ExportTunnelConfig(ctx context.Context, id string) (string, error)

	ListDNSRoutes(ctx context.Context) ([]DNSRoute, error)
	// GetDNSRoute returns one list in full — Domains uncapped, plus the
	// excludes and subscriptions the list view drops. The tool pages
	// Domains; implementations must not truncate them here.
	GetDNSRoute(ctx context.Context, id string) (DNSRouteDetail, error)
	AddDNSRoute(ctx context.Context, in DNSRouteInput) (DNSRoute, error)
	// UpdateDNSRoute applies a partial edit and returns the list as it
	// stands afterwards. warnings carries losses the edit caused that the
	// caller did not ask for — replacing a multi-target list's routes with
	// the single tunnel the input names.
	UpdateDNSRoute(ctx context.Context, in DNSRouteUpdate) (updated DNSRoute, warnings []string, err error)
	// SetDNSRouteEnabled toggles a list on or off and returns it as it
	// stands afterwards. Reversible: the record survives either way.
	SetDNSRouteEnabled(ctx context.Context, id string, enabled bool) (DNSRoute, error)
	// RemoveDNSRoute deletes the list and returns it as it was; the
	// deletion is permanent, so the record is the only thing left to show
	// the user. See tools_routing.go removedDNSOut.
	RemoveDNSRoute(ctx context.Context, id string) (DNSRoute, error)

	ListStaticRoutes(ctx context.Context) ([]StaticRoute, error)
	AddStaticRoute(ctx context.Context, in StaticRouteInput) (StaticRoute, error)
	// SetStaticRouteEnabled is SetDNSRouteEnabled for subnet lists.
	SetStaticRouteEnabled(ctx context.Context, id string, enabled bool) (StaticRoute, error)
	// RemoveStaticRoute deletes the list and returns it as it was.
	RemoveStaticRoute(ctx context.Context, id string) (StaticRoute, error)

	ListClientRoutes(ctx context.Context) ([]ClientRoute, error)
	SetClientRoute(ctx context.Context, in ClientRouteInput) (*ClientRoute, error) // nil when removed
	// SetClientRouteEnabled switches one device's route without removing
	// it. Addressed by client IP, as SetClientRoute is: that is what the
	// device list gives an agent. The IP is already canonical here.
	SetClientRouteEnabled(ctx context.Context, clientIP string, enabled bool) (ClientRoute, error)

	ListAccessPolicies(ctx context.Context) ([]AccessPolicy, error)
	ListDevices(ctx context.Context) ([]Device, error)

	GetLogs(ctx context.Context, q LogsQuery) ([]LogEntry, int, error) // entries, total matched
	TestConnectivity(ctx context.Context, tunnelID string) (ConnectivityResult, error)
	// CheckIP fetches the external IP through the tunnel and over the bare
	// WAN. A tunnel that is not running is an error, not an empty result.
	CheckIP(ctx context.Context, tunnelID string) (IPCheckResult, error)
	MonitoringMatrix(ctx context.Context) (MonitoringMatrix, error)
	// RunPingCheck starts a check of every monitored tunnel without
	// waiting for it and returns the last completed statuses.
	RunPingCheck(ctx context.Context) (PingCheckRun, error)

	// ResolveDomain looks a domain up (IPv4 only), for explain_route's
	// subnet comparison. A lookup failure is returned as an error; the
	// tool degrades rather than failing the whole call.
	ResolveDomain(ctx context.Context, domain string) ([]string, error)

	ListManagedServers(ctx context.Context) ([]ManagedServer, error)
	ControlSingbox(ctx context.Context, action string) (SingboxStatus, error)
	ListSingboxTunnels(ctx context.Context) ([]SingboxTunnel, error)
	// CheckSingboxDelay probes one proxy. An unknown tag is an error;
	// a proxy that stays silent is a result with Reachable false.
	CheckSingboxDelay(ctx context.Context, tag string) (SingboxDelay, error)

	OpenAPISpec() []byte
}
