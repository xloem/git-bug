package commands

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/MichaelMure/git-bug/input"
)

type userCreateOptions struct {
	name     string
	email    string
	avatar   string
	login    string
	metadata map[string]string
	flags    *pflag.FlagSet
}

func newUserCreateCommand() *cobra.Command {
	env := newEnv()
	options := userCreateOptions{}

	cmd := &cobra.Command{
		Use:      "create",
		Short:    "Create a new identity.",
		PreRunE:  loadBackend(env),
		PostRunE: closeBackend(env),
		RunE: func(cmd *cobra.Command, args []string) error {
			options.flags = cmd.Flags()
			return runUserCreate(env, options)
		},
	}

	flags := cmd.Flags()
	flags.SortFlags = false

	flags.StringVarP(&options.name, "name", "n", "",
		"Provide the user's name instead of prompting")
	flags.StringVarP(&options.email, "email", "e", "",
		"Provide the user's email instead of prompting")
	flags.StringVarP(&options.avatar, "avatar", "a", "",
		"Provide the user's avatar url instead of prompting")
	flags.StringVarP(&options.login, "login", "l", "",
		"Provide a login for the user")
	flags.StringToStringVarP(&options.metadata, "metadata", "m", nil,
		"Provide metadata to associate with the user")

	return cmd
}

func runUserCreate(env *Env, opts userCreateOptions) error {
	var err error

	name := opts.name
	if opts.flags.Lookup("name") == nil {
		preName, err := env.backend.GetUserName()
		if err != nil {
			return err
		}

		name, err = input.PromptDefault("Name", "name", preName, input.Required)
		if err != nil {
			return err
		}
	}

	email := opts.email
	if opts.flags.Lookup("email") == nil {
		preEmail, err := env.backend.GetUserEmail()
		if err != nil {
			return err
		}
	
		email, err = input.PromptDefault("Email", "email", preEmail, input.Required)
		if err != nil {
			return err
		}
	}

	avatarURL := opts.avatar
	if opts.flags.Lookup("avatar") == nil {
		avatarURL, err = input.Prompt("Avatar URL", "avatar")
		if err != nil {
			return err
		}
	}
	
	id, err := env.backend.NewIdentityRaw(name, email, opts.login, avatarURL, opts.metadata)
	if err != nil {
		return err
	}

	err = id.CommitAsNeeded()
	if err != nil {
		return err
	}

	set, err := env.backend.IsUserIdentitySet()
	if err != nil {
		return err
	}

	if !set {
		err = env.backend.SetUserIdentity(id)
		if err != nil {
			return err
		}
	}

	env.err.Println()
	env.out.Println(id.Id())

	return nil
}
