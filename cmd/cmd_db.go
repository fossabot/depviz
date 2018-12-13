package main

import (
	"encoding/json"
	"fmt"
	"moul.io/depviz/pkg/issues"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

type dbOptions struct{}

func (opts dbOptions) String() string {
	out, _ := json.Marshal(opts)
	return string(out)
}

type dbCommand struct {
	opts dbOptions
}

func (cmd *dbCommand) LoadDefaultOptions() error {
	if err := viper.Unmarshal(&cmd.opts); err != nil {
		return err
	}
	return nil
}

func (cmd *dbCommand) NewCobraCommand(dc map[string]DepvizCommand) *cobra.Command {
	cc := &cobra.Command{
		Use: "db",
	}
	cc.AddCommand(cmd.dbDumpCommand())
	return cc
}

func (cmd *dbCommand) ParseFlags(flags *pflag.FlagSet) {
	viper.BindPFlags(flags)
}

func (cmd *dbCommand) dbDumpCommand() *cobra.Command {
	cc := &cobra.Command{
		Use: "dump",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := cmd.opts
			return dbDump(&opts)
		},
	}
	cmd.ParseFlags(cc.Flags())
	return cc
}

func dbDump(opts *dbOptions) error {
	issues := []*issues.Issue{}
	if err := db.Find(&issues).Error; err != nil {
		return err
	}

	for _, issue := range issues {
		issue.PostLoad()
	}

	out, err := json.MarshalIndent(issues, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}