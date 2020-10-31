package commands

import (
	"github.com/spf13/cobra"

	"github.com/MichaelMure/git-bug/input"

	"time"
)

type addOptions struct {
	title        string
	message      string
	messageFile  string
	unixTime     int64
	metadata     map[string]string
	metadataFile map[string]string
}

func newAddCommand() *cobra.Command {
	env := newEnv()
	options := addOptions{}

	cmd := &cobra.Command{
		Use:      "add",
		Short:    "Create a new bug.",
		PreRunE:  loadBackendEnsureUser(env),
		PostRunE: closeBackend(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAdd(env, options)
		},
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.title, "title", "t", "",
		"Provide a title to describe the issue")
	flags.StringVarP(&options.message, "message", "m", "",
		"Provide a message to describe the issue")
	flags.StringVarP(&options.messageFile, "file", "F", "",
		"Take the message from the given file. Use - to read the message from the standard input")
	flags.Int64VarP(&options.unixTime, "time", "u", 0,
		"Set the unix timestamp of the commit, in seconds since 1970-01-01")
	flags.StringToStringVarP(&options.metadata, "metadata", "d", make(map[string]string),
		"Provide metadata to associate with the bug")
	flags.StringToStringVarP(&options.metadataFile, "metadatafile", "D", nil,
		"Provide filenames of metadata to associate with the bug")

	return cmd
}

func runAdd(env *Env, opts addOptions) error {
	var err error
	if opts.messageFile != "" && opts.message == "" {
		opts.title, opts.message, err = input.BugCreateFileInput(opts.messageFile)
		if err != nil {
			return err
		}
	}

	if opts.messageFile == "" && (opts.message == "" || opts.title == "") {
		opts.title, opts.message, err = input.BugCreateEditorInput(env.backend, opts.title, opts.message)

		if err == input.ErrEmptyTitle {
			env.out.Println("Empty title, aborting.")
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

	b, _, err := env.backend.NewBugRawForUser(opts.unixTime, opts.title, opts.message, nil, opts.metadata)

	if err != nil {
		return err
	}

	env.out.Printf("%s created\n", b.Id().Human())

	return nil
}
