package cli

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"herdlite/internal/app"
	"herdlite/internal/daemon"
	"herdlite/internal/debugui"
	"herdlite/internal/state"
)

func runMail(a *app.App, args []string) int {
	if len(args) == 0 {
		return runMailList(a, nil)
	}
	if hasHelp(args) {
		printMailHelp(a)
		return 0
	}

	switch args[0] {
	case "list":
		return runMailList(a, args[1:])
	case "show":
		return runMailShow(a, args[1:])
	case "raw":
		return runMailRaw(a, args[1:])
	case "open":
		return runMailOpen(a, args[1:])
	case "clear":
		return runMailClear(a, args[1:])
	default:
		fmt.Fprintf(a.Err, "unknown mail command: %s\n\n", args[0])
		printMailHelp(a)
		return 1
	}
}

func printMailHelp(a *app.App) {
	fmt.Fprintln(a.Out, "Usage:")
	fmt.Fprintln(a.Out, "  herdlite mail")
	fmt.Fprintln(a.Out, "  herdlite mail list [--p <path|name|domain>] [--all] [--unknown]")
	fmt.Fprintln(a.Out, "  herdlite mail show <id>")
	fmt.Fprintln(a.Out, "  herdlite mail raw <id>")
	fmt.Fprintln(a.Out, "  herdlite mail open <id>")
	fmt.Fprintln(a.Out, "  herdlite mail clear [--p <path|name|domain>] [--all] [--unknown] [--yes]")
}

func runMailList(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite mail list [--p <path|name|domain>] [--all] [--unknown]")
		return 0
	}
	filter, err := mailFilterFromArgs(a, args, false)
	if err != nil {
		fmt.Fprintf(a.Err, "mail list: %v\n", err)
		return 1
	}
	messages, err := a.Store.MailMessages(filter)
	if err != nil {
		fmt.Fprintf(a.Err, "mail list: %v\n", err)
		return 1
	}
	if len(messages) == 0 {
		fmt.Fprintln(a.Out, "No mail captured.")
		return 0
	}

	w := tabwriter.NewWriter(a.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tPROJECT\tTIME\tFROM\tSUBJECT\tATTACH")
	for _, message := range messages {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%d\n",
			message.ID,
			message.ProjectName,
			message.ReceivedAt.Local().Format("2006-01-02 15:04"),
			message.Sender,
			compact(message.Subject, 48),
			len(message.Attachments),
		)
	}
	w.Flush()
	return 0
}

func runMailShow(a *app.App, args []string) int {
	id, ok := mailIDArg(a, "show", args)
	if !ok {
		return codeForUsage(args, 1)
	}
	message, found, err := a.Store.MailMessage(id)
	if err != nil {
		fmt.Fprintf(a.Err, "mail show: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(a.Err, "mail show: message %d not found\n", id)
		return 1
	}

	fmt.Fprintf(a.Out, "ID:          %d\n", message.ID)
	fmt.Fprintf(a.Out, "Project:     %s\n", message.ProjectName)
	fmt.Fprintf(a.Out, "Received:    %s\n", message.ReceivedAt.Local().Format(time.RFC1123))
	fmt.Fprintf(a.Out, "From:        %s\n", message.Sender)
	if message.ReplyTo != "" {
		fmt.Fprintf(a.Out, "Reply-To:    %s\n", message.ReplyTo)
	}
	fmt.Fprintf(a.Out, "Recipients:  %s\n", message.Recipients)
	fmt.Fprintf(a.Out, "Subject:     %s\n", message.Subject)
	if len(message.Attachments) > 0 {
		fmt.Fprintln(a.Out, "Attachments:")
		for _, attachment := range message.Attachments {
			fmt.Fprintf(a.Out, "  #%d %s %s %d bytes\n", attachment.ID, attachment.Filename, attachment.ContentType, attachment.Size)
		}
	}
	fmt.Fprintln(a.Out)
	if message.TextBody != "" {
		fmt.Fprintln(a.Out, message.TextBody)
	} else if message.HTMLBody != "" {
		fmt.Fprintln(a.Out, "[HTML only] Use `herdlite mail open <id>` to view it in the browser.")
	}
	return 0
}

func runMailRaw(a *app.App, args []string) int {
	id, ok := mailIDArg(a, "raw", args)
	if !ok {
		return codeForUsage(args, 1)
	}
	message, found, err := a.Store.MailMessage(id)
	if err != nil {
		fmt.Fprintf(a.Err, "mail raw: %v\n", err)
		return 1
	}
	if !found {
		fmt.Fprintf(a.Err, "mail raw: message %d not found\n", id)
		return 1
	}
	fmt.Fprint(a.Out, string(message.RawMIME))
	if len(message.RawMIME) == 0 || message.RawMIME[len(message.RawMIME)-1] != '\n' {
		fmt.Fprintln(a.Out)
	}
	return 0
}

func runMailOpen(a *app.App, args []string) int {
	id, ok := mailIDArg(a, "open", args)
	if !ok {
		return codeForUsage(args, 1)
	}
	if !(daemon.Manager{Paths: a.Paths, Store: a.Store, Out: a.Out}).Status().Healthy {
		fmt.Fprintln(a.Err, "mail open: mail viewer is not running; run `herdlite start` first")
		return 1
	}
	if _, found, err := a.Store.MailMessage(id); err != nil {
		fmt.Fprintf(a.Err, "mail open: %v\n", err)
		return 1
	} else if !found {
		fmt.Fprintf(a.Err, "mail open: message %d not found\n", id)
		return 1
	}
	opener, err := exec.LookPath("xdg-open")
	if err != nil {
		fmt.Fprintf(a.Err, "mail open: xdg-open not found\n")
		return 1
	}
	url := fmt.Sprintf("%s/app/mail/%d", debugui.BaseURL, id)
	if err := exec.Command(opener, url).Start(); err != nil {
		fmt.Fprintf(a.Err, "mail open: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Opened %s\n", url)
	return 0
}

func runMailClear(a *app.App, args []string) int {
	if hasHelp(args) {
		fmt.Fprintln(a.Out, "Usage: herdlite mail clear [--p <path|name|domain>] [--all] [--unknown] [--yes]")
		return 0
	}
	yes := false
	remaining := []string{}
	for _, arg := range args {
		if arg == "--yes" || arg == "-y" {
			yes = true
			continue
		}
		remaining = append(remaining, arg)
	}
	filter, err := mailFilterFromArgs(a, remaining, true)
	if err != nil {
		fmt.Fprintf(a.Err, "mail clear: %v\n", err)
		return 1
	}
	if !yes && !confirm(a, "Clear matching captured mail?") {
		fmt.Fprintln(a.Out, "Cancelled.")
		return 0
	}
	count, err := a.Store.ClearMailMessages(filter)
	if err != nil {
		fmt.Fprintf(a.Err, "mail clear: %v\n", err)
		return 1
	}
	fmt.Fprintf(a.Out, "Cleared %d message(s).\n", count)
	return 0
}

func mailFilterFromArgs(a *app.App, args []string, strictClear bool) (state.MailFilter, error) {
	selector, args, err := splitProjectSelector(args)
	if err != nil {
		return state.MailFilter{}, err
	}
	filter := state.MailFilter{}
	for _, arg := range args {
		switch arg {
		case "--all":
			filter.All = true
		case "--unknown":
			filter.UnknownOnly = true
		default:
			return state.MailFilter{}, fmt.Errorf("unknown option: %s", arg)
		}
	}
	if filter.All && filter.UnknownOnly {
		return state.MailFilter{}, fmt.Errorf("--all and --unknown cannot be combined")
	}
	if selector.Value != "" {
		if filter.All || filter.UnknownOnly {
			return state.MailFilter{}, fmt.Errorf("--p cannot be combined with --all or --unknown")
		}
		project, err := resolveExistingProject(a, selector)
		if err != nil {
			return state.MailFilter{}, err
		}
		filter.ProjectName = project.Name
	}
	if filter.All || filter.UnknownOnly || filter.ProjectName != "" {
		return filter, nil
	}
	project, found, err := a.Store.ProjectForWorkingDirectory(".")
	if err != nil {
		return state.MailFilter{}, err
	}
	if found {
		filter.ProjectName = project.Name
		return filter, nil
	}
	if strictClear {
		return state.MailFilter{}, fmt.Errorf("outside a linked project; pass --all or --unknown")
	}
	filter.All = true
	return filter, nil
}

func mailIDArg(a *app.App, name string, args []string) (int64, bool) {
	if hasHelp(args) || len(args) != 1 {
		fmt.Fprintf(a.Out, "Usage: herdlite mail %s <id>\n", name)
		return 0, false
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		fmt.Fprintf(a.Err, "mail %s: invalid id %q\n", name, args[0])
		return 0, false
	}
	return id, true
}

func confirm(a *app.App, question string) bool {
	fmt.Fprintf(a.Out, "%s [y/N] ", question)
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	return answer == "y" || answer == "yes"
}

func compact(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	if max <= 1 {
		return value[:max]
	}
	return value[:max-1] + "..."
}
