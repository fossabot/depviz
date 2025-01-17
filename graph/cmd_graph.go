package graph // import "moul.io/depviz/graph"

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"moul.io/depviz/cli"
	"moul.io/depviz/model"
	"moul.io/depviz/sql"
)

func GetOptions(commands cli.Commands) Options {
	return commands["graph"].(*graphCommand).opts
}

func Commands() cli.Commands {
	return cli.Commands{"graph": &graphCommand{}}
}

type graphCommand struct {
	opts Options
}

func (cmd *graphCommand) CobraCommand(commands cli.Commands) *cobra.Command {
	cc := &cobra.Command{
		Use:   "graph",
		Short: "Output graph of relationships between all issues stored in database",
		Args: func(c *cobra.Command, args []string) error {
			// FIXME: if no args, then graph the whole database
			if err := cobra.MinimumNArgs(1)(c, args); err != nil {
				return err
			}
			return nil
		},
		RunE: func(_ *cobra.Command, args []string) error {
			opts := cmd.opts
			opts.SQL = sql.GetOptions(commands)
			targets, err := model.ParseTargets(args)
			if err != nil {
				return err
			}
			opts.Targets = targets
			if err := opts.Validate(); err != nil {
				return err
			}
			return PrintGraph(&opts)
		},
	}
	cmd.ParseFlags(cc.Flags())
	commands["sql"].ParseFlags(cc.Flags())
	return cc
}

func (cmd *graphCommand) LoadDefaultOptions() error {
	return viper.Unmarshal(&cmd.opts)
}

func (cmd *graphCommand) ParseFlags(flags *pflag.FlagSet) {
	flags.BoolVarP(&cmd.opts.ShowClosed, "show-closed", "", false, "show closed issues/PRs")
	flags.BoolVarP(&cmd.opts.ShowOrphans, "show-orphans", "", false, "show orphans issues/PRs")
	flags.BoolVarP(&cmd.opts.ShowPRs, "show-prs", "", false, "show PRs")
	flags.BoolVarP(&cmd.opts.ShowAllRelated, "show-all-related", "", false, "show related from other repos")
	flags.BoolVarP(&cmd.opts.Vertical, "vertical", "", false, "display graph vertically instead of horizontally")
	flags.StringVarP(&cmd.opts.Format, "format", "f", "dot", "output format (dot, graphman-pert)")
	flags.BoolVarP(&cmd.opts.NoPertEstimates, "no-pert-estimates", "", false, "do not compute PERT estimates")
	if err := viper.BindPFlags(flags); err != nil {
		zap.L().Warn("failed to bind viper flags", zap.Error(err))
	}
}
