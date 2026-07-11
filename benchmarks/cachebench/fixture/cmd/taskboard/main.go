package main

import (
	"fmt"
	"os"

	"example.com/cachebench/taskboard/internal/taskboard"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: taskboard <list|stats>")
	}

	store := taskboard.NewStore("data/tasks.json")
	tasks, err := store.Load()
	if err != nil {
		return err
	}

	switch args[0] {
	case "list":
		for _, task := range tasks {
			fmt.Printf("%d\t%s\t%s\n", task.ID, task.Status, task.Title)
		}
	case "stats":
		for status, count := range taskboard.CountByStatus(tasks) {
			fmt.Printf("%s\t%d\n", status, count)
		}
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}
