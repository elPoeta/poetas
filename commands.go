package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

type command struct {
	description string
	usage       string
	run         func(args string)
}

var commands = map[string]command{}

func init() {
	commands["help"] = command{description: "show available commands", run: cmdHelp}
	commands["model"] = command{description: "show or change the model", run: cmdModel}
	commands["clear"] = command{description: "clear conversation history", run: cmdClear}
	//commands["tools"] = command{description: "list available tools", run: cmdTools}
	commands["exit"] = command{description: "exit the harness", run: cmdExit}
	commands["quit"] = command{description: "exit the harness", run: cmdExit}
}

func runCommand(line string) bool {
	if !strings.HasPrefix(line, "/") {
		return false
	}
	parts := strings.SplitN(strings.TrimPrefix(line, "/"), " ", 2)
	name := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	c, ok := commands[name]
	if !ok {
		fmt.Printf("unknown command: /%s (try /help)\n", name)
		return true
	}
	c.run(args)
	return true
}

func cmdHelp(_ string) {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		c := commands[n]
		display := "/" + n
		if c.usage != "" {
			display = c.usage
		}
		fmt.Printf("  %-22s %s\n", display, c.description)
	}
}

func cmdClear(_ string) {
	messages = messages[:0]
	fmt.Println("conversation cleared")
}

var knownModels = []string{
	"ornith-9b",
}

func cmdModel(args string) {
	if args == "" {
		fmt.Printf("current: %s\n", llm.Model())
		fmt.Println("suggestions:")
		for _, m := range knownModels {
			fmt.Printf("  %s\n", m)
		}
		return
	}
	llm.SetModel(args)
	fmt.Printf("model: %s\n", args)
}

func cmdExit(_ string) {
	os.Exit(0)
}
