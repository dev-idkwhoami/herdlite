package cli

import (
	"fmt"

	"herdlite/internal/app"
	"herdlite/internal/laravel"
)

func runEnv(a *app.App, args []string) int {
	if len(args) == 0 || hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite env apply [--p <path|name|domain>] [--database] [--mail] [--ws] [--all]")
		return codeForUsage(args, 1)
	}
	switch args[0] {
	case "apply":
		return runEnvApply(a, args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown env command: %s\n", args[0])
		return 1
	}
}

func runEnvApply(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite env apply [--p <path|name|domain>] [--database] [--mail] [--ws] [--all]")
		return 0
	}
	selector, args, err := splitProjectSelector(args)
	if err != nil {
		fmt.Fprintf(a.Err, "env apply: %v\n", err)
		return 1
	}
	applyDatabase := false
	applyMail := false
	applyWebsocket := false
	explicit := false
	for _, arg := range args {
		switch arg {
		case "--database":
			applyDatabase = true
			explicit = true
		case "--mail":
			applyMail = true
			explicit = true
		case "--ws":
			applyWebsocket = true
			explicit = true
		case "--all":
			applyDatabase = true
			applyMail = true
			applyWebsocket = true
			explicit = true
		default:
			fmt.Fprintf(a.Err, "unknown env apply option: %s\n", arg)
			return 1
		}
	}
	if !explicit {
		applyDatabase = true
		applyMail = true
		applyWebsocket = true
	}
	project, err := resolveExistingProject(a, selector)
	if err != nil {
		fmt.Fprintf(a.Err, "env apply: %v\n", err)
		return 1
	}

	values := laravel.AppEnv(project.Domain)
	if applyDatabase {
		mergeEnv(values, laravel.DatabaseEnv(projectDatabaseName(project)))
	}
	if applyMail {
		mergeEnv(values, laravel.MailEnv("noreply@"+project.Domain))
	}
	if applyWebsocket && project.Websocket.Enabled {
		mergeEnv(values, laravel.WebsocketEnv(project.Websocket.Domain))
	}
	if len(values) == 0 {
		fmt.Fprintln(a.Out, "No environment values to apply.")
		return 0
	}

	fmt.Fprintf(a.Out, "Applying environment values for %s\n", project.Name)
	for _, key := range sortedEnvKeysForPrint(values) {
		fmt.Fprintf(a.Out, "  %-22s %s\n", key, values[key])
	}
	if err := laravel.ApplyEnv(project.Path, values); err != nil {
		fmt.Fprintf(a.Err, "env apply: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Updated %s\n", project.Path+"/.env")
	return 0
}

func mergeEnv(target map[string]string, values map[string]string) {
	for key, value := range values {
		target[key] = value
	}
}

func sortedEnvKeysForPrint(values map[string]string) []string {
	preferred := []string{
		"APP_NAME", "APP_URL",
		"DB_CONNECTION", "DB_HOST", "DB_PORT", "DB_DATABASE", "DB_USERNAME", "DB_PASSWORD",
		"MAIL_MAILER", "MAIL_HOST", "MAIL_PORT", "MAIL_USERNAME", "MAIL_PASSWORD", "MAIL_ENCRYPTION", "MAIL_FROM_ADDRESS",
		"REVERB_HOST", "REVERB_PORT", "REVERB_SCHEME", "VITE_REVERB_HOST", "VITE_REVERB_PORT", "VITE_REVERB_SCHEME",
	}
	out := []string{}
	added := map[string]bool{}
	for _, key := range preferred {
		if _, ok := values[key]; ok {
			out = append(out, key)
			added[key] = true
		}
	}
	for key := range values {
		if !added[key] {
			out = append(out, key)
		}
	}
	return out
}
