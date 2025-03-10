package machine

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/superfly/flyctl/api"
	"github.com/superfly/flyctl/flaps"
	"github.com/superfly/flyctl/internal/command"
	"github.com/superfly/flyctl/internal/flag"
	"github.com/superfly/flyctl/iostreams"
)

func newStop() *cobra.Command {
	const (
		short = "Stop one or more Fly machines"
		long  = short + "\n"

		usage = "stop <id> [<id>...]"
	)

	cmd := command.New(usage, short, long, runMachineStop,
		command.RequireSession,
		command.LoadAppNameIfPresent,
	)

	cmd.Args = cobra.ArbitraryArgs

	flag.Add(
		cmd,
		flag.App(),
		flag.AppConfig(),
		selectFlag,
	)

	return cmd
}

func runMachineStop(ctx context.Context) (err error) {
	var (
		io   = iostreams.FromContext(ctx)
		args = flag.Args(ctx)
	)

	machineIDs, ctx, err := selectManyMachineIDs(ctx, args)
	if err != nil {
		return err
	}

	for _, machineID := range machineIDs {
		fmt.Fprintf(io.Out, "Sending kill signal to machine %s...\n", machineID)

		if err = Stop(ctx, machineID); err != nil {
			return
		}
		fmt.Fprintf(io.Out, "%s has been successfully stopped\n", machineID)
	}
	return
}

func Stop(ctx context.Context, machineID string) (err error) {
	machineStopInput := api.StopMachineInput{
		ID:      machineID,
		Filters: &api.Filters{},
	}

	err = flaps.FromContext(ctx).Stop(ctx, machineStopInput)
	if err != nil {
		if err := rewriteMachineNotFoundErrors(ctx, err, machineID); err != nil {
			return err
		}
		return fmt.Errorf("could not stop machine %s: %w", machineID, err)
	}

	return
}
