package ips

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/superfly/flyctl/client"
	"github.com/superfly/flyctl/internal/appconfig"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/config"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/internal/render"
	"github.com/superfly/flyctl/iostreams"
)

func newList() *cobra.Command {
	const (
		long  = `Lists the IP addresses allocated to the application`
		short = `List allocated IP addresses`
	)

	cmd := command.New("list", short, long, runIPAddressesList,
		command.RequireSession,
		command.RequireAppName,
	)

	flag.Add(cmd,
		flag.App(),
		flag.AppConfig(),
	)
	return cmd
}

func runIPAddressesList(ctx context.Context) error {
	cfg := config.FromContext(ctx)
	client := client.FromContext(ctx).API()

	appName := appconfig.NameFromContext(ctx)
	ipAddresses, err := client.GetIPAddresses(ctx, appName)
	if err != nil {
		return err
	}

	if cfg.JSONOutput {
		out := iostreams.FromContext(ctx).Out
		return render.JSON(out, ipAddresses)
	}

	renderListTable(ctx, ipAddresses)
	return nil
}
