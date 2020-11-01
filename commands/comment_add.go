package commands

import (
	"github.com/spf13/cobra"

	_select "github.com/MichaelMure/git-bug/commands/select"
	"github.com/MichaelMure/git-bug/input"

	"time"
)

type commentAddOptions struct {
	messageFile  string
	message      string
	unixTime     int64
	metadata     map[string]string
	metadataFile map[string]string
}

func newCommentAddCommand() *cobra.Command {
	env := newEnv()
	options := commentAddOptions{}

	cmd := &cobra.Command{
		Use:      "add [ID]",
		Short:    "Add a new comment to a bug.",
		PreRunE:  loadBackendEnsureUser(env),
		PostRunE: closeBackend(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCommentAdd(env, options, args)
		},
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.messageFile, "file", "F", "",
		"Take the message from the given file. Use - to read the message from the standard input")

	flags.StringVarP(&options.message, "message", "m", "",
		"Provide the new message from the command line")

	flags.Int64VarP(&options.unixTime, "time", "u", 0,
		"Set the unix timestamp of the commit, in seconds since 1970-01-01")

	flags.StringToStringVarP(&options.metadata, "metadata", "d", make(map[string]string),
		"Provide metadata to associate with the bug")

	flags.StringToStringVarP(&options.metadataFile, "metadatafile", "D", nil,
		"Provide filenames of metadata to associate with the bug")

	return cmd
}

func runCommentAdd(env *Env, opts commentAddOptions, args []string) error {
	b, args, err := _select.ResolveBug(env.backend, args)
	if err != nil {
		return err
	}

	if opts.messageFile != "" && opts.message == "" {
		opts.message, err = input.BugCommentFileInput(opts.messageFile)
		if err != nil {
			return err
		}
	}

	if opts.messageFile == "" && opts.message == "" {
		opts.message, err = input.BugCommentEditorInput(env.backend, "")
		if err == input.ErrEmptyMessage {
			env.err.Println("Empty message, aborting.")
			return nil
		}
		if err != nil {
			return err
		}
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

	_, err = b.AddCommentRawForUser(opts.unixTime, opts.message, nil, opts.metadata)
	if err != nil {
		return err
	}

	return b.Commit()
}
