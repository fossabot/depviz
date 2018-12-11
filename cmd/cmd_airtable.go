package main

import (
	"encoding/json"
	"fmt"

	"github.com/brianloveswords/airtable"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
	"go.uber.org/zap"
	"moul.io/depviz/pkg/airtabledb"
	"moul.io/depviz/pkg/repo"
)

type airtableOptions struct {
	IssuesTableName       string `mapstructure:"airtable-issues-table-name"`
	RepositoriesTableName string `mapstructure:"airtable-repositories-table-name"`
	LabelsTableName       string `mapstructure:"airtable-labels-table-name"`
	MilestonesTableName   string `mapstructure:"airtable-milestones-table-name"`
	ProvidersTableName    string `mapstructure:"airtable-providers-table-name"`
	AccountsTableName     string `mapstructure:"airtable-accounts-table-name"`
	BaseID                string `mapstructure:"airtable-base-id"`
	Token                 string `mapstructure:"airtable-token"`
	DestroyInvalidRecords bool   `mapstructure:"airtable-destroy-invalid-records"`
	TableNames            []string

	Targets []repo.Target `mapstructure:"targets"`
}

func (opts airtableOptions) String() string {
	out, _ := json.Marshal(opts)
	return string(out)
}

type airtableCommand struct {
	opts airtableOptions
}

func (cmd *airtableCommand) LoadDefaultOptions() error {
	if err := viper.Unmarshal(&cmd.opts); err != nil {
		return err
	}
	return nil
}

func (cmd *airtableCommand) ParseFlags(flags *pflag.FlagSet) {
	cmd.opts.TableNames = make([]string, airtabledb.NumTables)

	flags.StringVarP(&cmd.opts.IssuesTableName, "airtable-issues-table-name", "", "Issues and PRs", "Airtable issues table name")
	cmd.opts.TableNames[airtabledb.IssueIndex] = cmd.opts.IssuesTableName
	flags.StringVarP(&cmd.opts.RepositoriesTableName, "airtable-repositories-table-name", "", "Repositories", "Airtable repositories table name")
	cmd.opts.TableNames[airtabledb.RepositoryIndex] = cmd.opts.RepositoriesTableName
	flags.StringVarP(&cmd.opts.AccountsTableName, "airtable-accounts-table-name", "", "Accounts", "Airtable accounts table name")
	cmd.opts.TableNames[airtabledb.AccountIndex] = cmd.opts.AccountsTableName
	flags.StringVarP(&cmd.opts.LabelsTableName, "airtable-labels-table-name", "", "Labels", "Airtable labels table name")
	cmd.opts.TableNames[airtabledb.LabelIndex] = cmd.opts.LabelsTableName
	flags.StringVarP(&cmd.opts.MilestonesTableName, "airtable-milestones-table-name", "", "Milestones", "Airtable milestones table nfame")
	cmd.opts.TableNames[airtabledb.MilestoneIndex] = cmd.opts.MilestonesTableName
	flags.StringVarP(&cmd.opts.ProvidersTableName, "airtable-providers-table-name", "", "Providers", "Airtable providers table name")
	cmd.opts.TableNames[airtabledb.ProviderIndex] = cmd.opts.ProvidersTableName
	flags.StringVarP(&cmd.opts.BaseID, "airtable-base-id", "", "", "Airtable base ID")
	flags.StringVarP(&cmd.opts.Token, "airtable-token", "", "", "Airtable token")
	flags.BoolVarP(&cmd.opts.DestroyInvalidRecords, "airtable-destroy-invalid-records", "", false, "Destroy invalid records")
	viper.BindPFlags(flags)
}

func (cmd *airtableCommand) NewCobraCommand(dc map[string]DepvizCommand) *cobra.Command {
	cc := &cobra.Command{
		Use: "airtable",
	}
	cc.AddCommand(cmd.airtableSyncCommand())
	return cc
}

func (cmd *airtableCommand) airtableSyncCommand() *cobra.Command {
	cc := &cobra.Command{
		Use: "sync",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := cmd.opts
			var err error
			if opts.Targets, err = repo.ParseTargets(args); err != nil {
				return errors.Wrap(err, "invalid targets")
			}
			return airtableSync(&opts)
		},
	}
	cmd.ParseFlags(cc.Flags())
	return cc
}

func airtableSync(opts *airtableOptions) error {
	if opts.BaseID == "" || opts.Token == "" {
		return fmt.Errorf("missing token or baseid, check '-h'")
	}

	//
	// prepare
	//

	// load issues
	issues, err := loadIssues(nil)
	if err != nil {
		return errors.Wrap(err, "failed to load issues")
	}
	filtered := issues.FilterByTargets(opts.Targets)
	zap.L().Debug("fetch db entries", zap.Int("count", len(filtered)))

	// unique entries
	features := make([]map[string]repo.Feature, airtabledb.NumTables)
	for i, _ := range features {
		features[i] = make(map[string]repo.Feature)
	}

	for _, issue := range filtered {
		// providers
		features[airtabledb.ProviderIndex][issue.Repository.Provider.ID] = issue.Repository.Provider

		// labels
		for _, label := range issue.Labels {
			features[airtabledb.LabelIndex][label.ID] = label
		}

		// accounts
		if issue.Repository.Owner != nil {
			features[airtabledb.AccountIndex][issue.Repository.Owner.ID] = issue.Repository.Owner
		}

		features[airtabledb.AccountIndex][issue.Author.ID] = issue.Author
		for _, assignee := range issue.Assignees {
			features[airtabledb.AccountIndex][assignee.ID] = assignee
		}
		if issue.Milestone != nil && issue.Milestone.Creator != nil {
			features[airtabledb.AccountIndex][issue.Milestone.Creator.ID] = issue.Milestone.Creator
		}

		// repositories
		features[airtabledb.RepositoryIndex][issue.Repository.ID] = issue.Repository
		// FIXME: find external repositories based on depends-on links

		// milestones
		if issue.Milestone != nil {
			features[airtabledb.MilestoneIndex][issue.Milestone.ID] = issue.Milestone
		}

		// issue
		features[airtabledb.IssueIndex][issue.ID] = issue
		// FIXME: find external issues based on depends-on links
	}

	// init client
	at := airtable.Client{
		APIKey:  opts.Token,
		BaseID:  opts.BaseID,
		Limiter: airtable.RateLimiter(5),
	}

	// fetch remote data
	cache := airtabledb.NewDB()
	for tableKind, tableName := range opts.TableNames {
		table := at.Table(tableName)
		if err := cache.Tables[tableKind].Fetch(table); err != nil {
			return err
		}
	}

	unmatched := airtabledb.NewDB()

	//
	// compute fields
	//

	for tableKind, featureMap := range features {
		for _, dbEntry := range featureMap {
			matched := false
			dbRecord := dbEntry.ToRecord(cache)
			for idx := 0; idx < cache.Tables[tableKind].Len(); idx ++ {
				t := cache.Tables[tableKind]
				if t.GetFieldID(idx) == dbEntry.GetID() {
					if t.RecordsEqual(idx, dbRecord) {
						t.SetState(idx, airtabledb.StateUnchanged)
					} else {
						t.CopyFields(idx, dbRecord)
						t.SetState(idx, airtabledb.StateChanged)
					}
					matched = true
					break
				}
			}
			if !matched {
				unmatched.Tables[tableKind].Append(dbRecord)
			}
		}
	}

	//
	// update airtable
	//
	for tableKind, tableName := range opts.TableNames {
		table := at.Table(tableName)
		ut := unmatched.Tables[tableKind]
		ct := cache.Tables[tableKind]
		for i := 0; i < ut.Len(); i++ {
			zap.L().Debug("create airtable entry", zap.String("type", tableName), zap.String("entry", ut.StringAt(i)))
			if err := table.Create(ut.GetPtr(i)); err != nil {
				return err
			}
			ut.SetState(i, airtabledb.StateNew)
			ct.Append(ut.Get(i))
		}
		for i := 0; i < ct.Len(); i++ {
			var err error
			switch ct.GetState(i) {
			case airtabledb.StateUnknown:
				err = table.Delete(ct.GetPtr(i))
				zap.L().Debug("delete airtable entry", zap.String("type", tableName), zap.String("entry", ct.StringAt(i)), zap.Error(err))
			case airtabledb.StateChanged:
				err = table.Update(ct.GetPtr(i))
				zap.L().Debug("update airtable entry", zap.String("type", tableName), zap.String("entry", ct.StringAt(i)), zap.Error(err))
			case airtabledb.StateUnchanged:
				zap.L().Debug("unchanged airtable entry", zap.String("type", tableName), zap.String("entry", ct.StringAt(i)), zap.Error(err))
				// do nothing
			case airtabledb.StateNew:
				zap.L().Debug("new airtable entry", zap.String("type", tableName), zap.String("entry", ct.StringAt(i)), zap.Error(err))
				// do nothing
			}
		}
	}

	//
	// debug
	//
	for tableKind, tableName := range opts.TableNames {
		fmt.Println("-------", tableName)
		ct := cache.Tables[tableKind]
		for i := 0; i < ct.Len(); i++ {
			fmt.Println(ct.GetID(i), airtabledb.StateString[ct.GetState(i)], ct.GetFieldID(i))
		}
	}
	fmt.Println("-------")

	return nil
}
