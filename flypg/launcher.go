package flypg

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"time"

	"github.com/superfly/flyctl/ssh"

	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/helpers"

	mach "github.com/superfly/flyctl/internal/machine"
	"github.com/superfly/flyctl/internal/watch"

	"github.com/superfly/flyctl/flaps"
	iostreams "github.com/superfly/flyctl/iostreams"
)

var (
	volumeName     = "pg_data"
	volumePath     = "/data"
	duration10s, _ = time.ParseDuration("10s")
	duration15s, _ = time.ParseDuration("15s")
	checkPathPg    = "/flycheck/pg"
	checkPathRole  = "/flycheck/role"
	checkPathVm    = "/flycheck/vm"
)

const (
	ReplicationManager = "repmgr"
	StolonManager      = "stolon"
)

type Launcher struct {
	client *api.Client
}

type CreateClusterInput struct {
	AppName            string
	ConsulURL          string
	ImageRef           string
	InitialClusterSize int
	Organization       *api.Organization
	Password           string
	Region             string
	VolumeSize         *int
	VMSize             *api.VMSize
	SnapshotID         *string
	Manager            string
}

func NewLauncher(client *api.Client) *Launcher {
	return &Launcher{
		client: client,
	}
}

// Launches a postgres cluster using the machines runtime
func (l *Launcher) LaunchMachinesPostgres(ctx context.Context, config *CreateClusterInput, detach bool) error {
	var (
		io       = iostreams.FromContext(ctx)
		colorize = io.ColorScheme()
		client   = client.FromContext(ctx).API()
	)

	app, err := l.createApp(ctx, config)
	if err != nil {
		return err
	}

	// In case the user hasn't specified a name, use the app name generated by the API
	config.AppName = app.Name

	var addr *api.IPAddress

	if config.Manager == ReplicationManager {
		addr, err = l.client.AllocateIPAddress(ctx, config.AppName, "private_v6", config.Region, config.Organization, "")
		if err != nil {
			return err
		}
	}

	secrets, err := l.setSecrets(ctx, config)
	if err != nil {
		return err
	}

	flapsClient, err := flaps.New(ctx, app)
	if err != nil {
		return err
	}
	ctx = flaps.NewContext(ctx, flapsClient)

	nodes := make([]*api.Machine, 0)

	for i := 0; i < config.InitialClusterSize; i++ {
		machineConf := l.getPostgresConfig(config)

		machineConf.Image = config.ImageRef
		if machineConf.Image == "" {
			imageRepo := "flyio/postgres"

			if config.Manager == ReplicationManager {
				imageRepo = "flyio/postgres-flex"
			}

			imageRef, err := client.GetLatestImageTag(ctx, imageRepo, config.SnapshotID)
			if err != nil {
				return err
			}
			machineConf.Image = imageRef
		}

		concurrency := &api.MachineServiceConcurrency{
			Type:      "connections",
			HardLimit: 1000,
			SoftLimit: 1000,
		}

		if config.Manager == ReplicationManager {
			var bouncerPort int = 5432
			var pgPort int = 5433
			machineConf.Services = []api.MachineService{
				{
					Protocol:     "tcp",
					InternalPort: 5432,
					Ports: []api.MachinePort{
						{
							Port: &bouncerPort,
							Handlers: []string{
								"pg_tls",
							},
							ForceHttps: false,
						},
					},
					Concurrency: concurrency,
				},
				{
					Protocol:     "tcp",
					InternalPort: 5433,
					Ports: []api.MachinePort{
						{
							Port: &pgPort,
							Handlers: []string{
								"pg_tls",
							},
							ForceHttps: false,
						},
					},
					Concurrency: concurrency,
				},
			}
		}

		snapshot := config.SnapshotID
		verb := "Provisioning"

		// When a snapshot is specified, we only want to pass it into the first volume created.
		if snapshot != nil {
			verb = "Restoring"
			if i > 0 {
				snapshot = nil
			}
		}

		fmt.Fprintf(io.Out, "%s %d of %d machines with image %s\n", verb, i+1, config.InitialClusterSize, machineConf.Image)

		volInput := api.CreateVolumeInput{
			AppID:             app.ID,
			Name:              volumeName,
			Region:            config.Region,
			SizeGb:            *config.VolumeSize,
			Encrypted:         true,
			RequireUniqueZone: false,
			SnapshotID:        snapshot,
		}

		vol, err := l.client.CreateVolume(ctx, volInput)
		if err != nil {
			return err
		}

		machineConf.Mounts = append(machineConf.Mounts, api.MachineMount{
			Volume: vol.ID,
			Path:   volumePath,
		})

		launchInput := api.LaunchMachineInput{
			AppID:   app.ID,
			OrgSlug: config.Organization.ID,
			Region:  config.Region,
			Config:  machineConf,
		}

		machine, err := flapsClient.Launch(ctx, launchInput)
		if err != nil {
			return err
		}

		fmt.Fprintf(io.Out, "Waiting for machine to start...\n")

		waitTimeout := time.Minute * 5
		if snapshot != nil {
			waitTimeout = time.Hour
		}

		err = mach.WaitForStartOrStop(ctx, machine, "start", waitTimeout)
		if err != nil {
			return err
		}
		nodes = append(nodes, machine)

		fmt.Fprintf(io.Out, "Machine %s is %s\n", machine.ID, machine.State)

	}

	if !detach {
		fmt.Fprintln(io.Out, colorize.Green("==> "+"Monitoring health checks"))

		if err := watch.MachinesChecks(ctx, nodes); err != nil {
			return err
		}
		fmt.Fprintln(io.Out)
	}

	connStr := fmt.Sprintf("postgres://postgres:%s@%s.internal:5432\n", secrets["OPERATOR_PASSWORD"], config.AppName)

	if config.Manager == ReplicationManager && addr != nil {
		connStr = fmt.Sprintf("postgres://postgres:%s@%s.flycast:5432\n", secrets["OPERATOR_PASSWORD"], config.AppName)
	}

	fmt.Fprintf(io.Out, "Postgres cluster %s created\n", config.AppName)
	fmt.Fprintf(io.Out, "  Username:    postgres\n")
	fmt.Fprintf(io.Out, "  Password:    %s\n", secrets["OPERATOR_PASSWORD"])
	fmt.Fprintf(io.Out, "  Hostname:    %s.internal\n", config.AppName)
	if addr != nil {
		fmt.Fprintf(io.Out, "  Flycast:     %s\n", addr.Address)
	}
	fmt.Fprintf(io.Out, "  Proxy port:  5432\n")
	fmt.Fprintf(io.Out, "  Postgres port:  5433\n")
	fmt.Fprintf(io.Out, "  Connection string: %s\n", connStr)
	fmt.Fprintln(io.Out, colorize.Italic("Save your credentials in a secure place -- you won't be able to see them again!"))

	fmt.Fprintln(io.Out)
	fmt.Fprintln(io.Out, colorize.Bold("Connect to postgres"))
	fmt.Fprintf(io.Out, "Any app within the %s organization can connect to this Postgres using the above connection string\n", config.Organization.Name)

	fmt.Fprintln(io.Out)
	fmt.Fprintln(io.Out, "Now that you've set up Postgres, here's what you need to understand: https://fly.io/docs/postgres/getting-started/what-you-should-know/")

	// TODO: wait for the cluster to be ready

	return nil
}

func (l *Launcher) getPostgresConfig(config *CreateClusterInput) *api.MachineConfig {
	machineConfig := api.MachineConfig{}

	// Set env
	machineConfig.Env = map[string]string{
		"PRIMARY_REGION": config.Region,
	}

	// Set VM resources
	machineConfig.Guest = &api.MachineGuest{
		CPUKind:  config.VMSize.CPUClass,
		CPUs:     int(config.VMSize.CPUCores),
		MemoryMB: config.VMSize.MemoryMB,
	}

	// Metrics
	machineConfig.Metrics = &api.MachineMetrics{
		Path: "/metrics",
		Port: 9187,
	}

	machineConfig.Checks = map[string]api.MachineCheck{
		"pg": {
			Port:     api.Pointer(5500),
			Type:     api.Pointer("http"),
			HTTPPath: &checkPathPg,
			Interval: &api.Duration{Duration: duration15s},
			Timeout:  &api.Duration{Duration: duration10s},
		},
		"role": {
			Port:     api.Pointer(5500),
			Type:     api.Pointer("http"),
			HTTPPath: &checkPathRole,
			Interval: &api.Duration{Duration: duration15s},
			Timeout:  &api.Duration{Duration: duration10s},
		},
		"vm": {
			Port:     api.Pointer(5500),
			Type:     api.Pointer("http"),
			HTTPPath: &checkPathVm,
			Interval: &api.Duration{Duration: duration15s},
			Timeout:  &api.Duration{Duration: duration10s},
		},
	}

	// Metadata
	machineConfig.Metadata = map[string]string{
		"fly_platform_version": "v2",
		"fly-managed-postgres": "true",
	}

	// Restart policy
	machineConfig.Restart.Policy = api.MachineRestartPolicyAlways

	return &machineConfig
}

func (l *Launcher) createApp(ctx context.Context, config *CreateClusterInput) (*api.AppCompact, error) {
	fmt.Println("Creating app...")
	appInput := api.CreateAppInput{
		OrganizationID:  config.Organization.ID,
		Name:            config.AppName,
		PreferredRegion: &config.Region,
		AppRoleID:       "postgres_cluster",
	}

	app, err := l.client.CreateApp(ctx, appInput)
	if err != nil {
		return nil, err
	}

	return &api.AppCompact{
		ID:       app.ID,
		Name:     app.Name,
		Status:   app.Status,
		Deployed: app.Deployed,
		Hostname: app.Hostname,
		AppURL:   app.AppURL,
		Organization: &api.OrganizationBasic{
			ID:   app.Organization.ID,
			Slug: app.Organization.Slug,
		},
	}, nil
}

func (l *Launcher) setSecrets(ctx context.Context, config *CreateClusterInput) (map[string]string, error) {
	out := iostreams.FromContext(ctx).Out

	fmt.Fprintf(out, "Setting secrets on app %s...\n", config.AppName)

	var suPassword, replPassword, opPassword string
	var err error

	suPassword, err = helpers.RandString(15)
	if err != nil {
		return nil, err
	}

	replPassword, err = helpers.RandString(15)
	if err != nil {
		return nil, err
	}

	opPassword, err = helpers.RandString(15)
	if err != nil {
		return nil, err
	}

	secrets := map[string]string{
		"SU_PASSWORD":       suPassword,
		"REPL_PASSWORD":     replPassword,
		"OPERATOR_PASSWORD": opPassword,
	}

	if config.Manager == ReplicationManager {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			return nil, err
		}

		app := api.App{Name: config.AppName}
		cert, err := l.client.IssueSSHCertificate(ctx, config.Organization, []string{"root", "fly", "postgres"}, []api.App{app}, nil, pub)
		if err != nil {
			return nil, err
		}

		pemkey := ssh.MarshalED25519PrivateKey(priv, "postgres inter-machine ssh")

		secrets["SSH_KEY"] = string(pemkey)
		secrets["SSH_CERT"] = cert.Certificate
	}

	if config.SnapshotID != nil {
		secrets["FLY_RESTORED_FROM"] = *config.SnapshotID
	}

	if config.ConsulURL == "" {
		consulURL, err := l.generateConsulURL(ctx, config)
		if err != nil {
			return nil, err
		}
		secrets["FLY_CONSUL_URL"] = consulURL
	} else {
		secrets["CONSUL_URL"] = config.ConsulURL
	}

	if config.Password != "" {
		secrets["OPERATOR_PASSWORD"] = config.Password
	}

	_, err = l.client.SetSecrets(ctx, config.AppName, secrets)

	return secrets, err
}

func (l *Launcher) generateConsulURL(ctx context.Context, config *CreateClusterInput) (string, error) {
	data, err := l.client.EnablePostgresConsul(ctx, config.AppName)
	if err != nil {
		return "", err
	}

	return data.ConsulURL, nil
}
