package commands

import (
	_select "github.com/MichaelMure/git-bug/commands/select"
	"github.com/spf13/cobra"

	"github.com/MichaelMure/git-bug/input"

	"time"
)

type statusOpenOptions struct {
	unixTime    int64
	metadata    map[string]string
	metadataFile map[string]string
}

func newStatusOpenCommand() *cobra.Command {
	env := newEnv()
	options := statusOpenOptions{}

	cmd := &cobra.Command{
		Use:      "open [ID]",
		Short:    "Mark a bug as open.",
		PreRunE:  loadBackendEnsureUser(env),
		PostRunE: closeBackend(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatusOpen(env, args, options)
		},
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.Int64VarP(&options.unixTime, "time", "u", 0,
		"Set the unix timestamp of a status change, in seconds since 1970-01-01")
	flags.StringToStringVarP(&options.metadata, "metadata", "d", nil,
		"Provide metadata to associate with the status change")
	flags.StringToStringVarP(&options.metadataFile, "metadatafile", "D", nil,
		"Provide filenames of metadata to associate with the status change")

	return cmd
}

func runStatusOpen(env *Env, args []string, opts statusOpenOptions) error {
	b, args, err := _select.ResolveBug(env.backend, args)
	if err != nil {
		return err
	}

	if opts.unixTime == 0 {
		opts.unixTime = time.Now().Unix()
	}

	if opts.metadataFile != nil {
		for name, metadataFile := range opts.metadataFile {
			metadata, err := input.FileInput(metadataFile)
			if err != nil {
				return err
			}
			opts.metadata[name] = metadata
		}
	}

	if len(opts.metadata) == 0 {
		opts.metadata = nil
	}

	_, err = b.OpenRawForUser(opts.unixTime, opts.metadata)
	if err != nil {
		return err
	}

	return b.Commit()
}
