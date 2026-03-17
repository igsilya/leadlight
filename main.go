package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"leadlight/tui"
)

func main() {
	columns := []tui.ColumnDef{
		{Title: "ID", Percentage: 0.10},
		{Title: "Name", Percentage: 0.25},
		{Title: "Status", Percentage: 0.20},
		{Title: "Description", Percentage: 0.45},
	}

	rows := []tui.RowData{
		{
			Data:  []string{"1", "Lorem", "Active", "Ipsum dolor"},
			Style: tui.RowStyle{Background: "green"},
			SubRows: [][]string{
				{"1.1", "Sub lorem", "", "Detail ipsum"},
				{"1.2", "Sub dolor", "", "Detail amet"},
			},
		},
		{
			Data:  []string{"2", "Dolor", "Inactive", "Sit amet"},
			Style: tui.RowStyle{Background: "grey", Italic: true},
			SubRows: [][]string{
				{"2.1", "Sub sit", "", "Detail consect"},
			},
		},
		{
			Data:  []string{"3", "Amet", "Active", "Consectetur"},
			Style: tui.RowStyle{Background: "yellow", Bold: true},
			SubRows: [][]string{
				{"3.1", "Sub consect", "", "Detail adipis"},
				{"3.2", "Sub adipis", "", "Detail eiusmod"},
			},
		},
		{
			Data:  []string{"4", "Consect", "Active", "Adipiscing"},
			Style: tui.RowStyle{Background: "white"},
			SubRows: [][]string{
				{"4.1", "Sub eiusmod", "", "Detail tempor"},
				{"4.2", "Sub tempor", "", "Detail incid"},
			},
		},
		{
			Data:  []string{"5", "Adipis", "Inactive", "Eiusmod"},
			Style: tui.RowStyle{Background: "black", Italic: true},
			SubRows: [][]string{
				{"5.1", "Sub incid", "", "Detail labore"},
			},
		},
		{
			Data:  []string{"6", "Eiusmod", "Active", "Tempor"},
			Style: tui.RowStyle{Background: "darkred"},
			SubRows: [][]string{
				{"6.1", "Sub labore", "", "Detail dolore"},
				{"6.2", "Sub dolore", "", "Detail magna"},
			},
		},
		{
			Data:  []string{"7", "Tempor", "Active", "Incididunt"},
			Style: tui.RowStyle{Background: "lightred"},
			SubRows: [][]string{
				{"7.1", "Sub magna", "", "Detail aliqua"},
				{"7.2", "Sub aliqua", "", "Detail veniam"},
			},
		},
		{
			Data:  []string{"8", "Incid", "Inactive", "Labore"},
			Style: tui.RowStyle{Background: "grey", Italic: true},
			SubRows: [][]string{
				{"8.1", "Sub veniam", "", "Detail nostrud"},
			},
		},
	}

	statusOptions := []string{
		"Active", "Inactive", "Pending", "Away",
	}

	m := tui.NewModel(columns, rows, statusOptions, 2)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
