package parser

import (
	"regexp"
	"strconv"
	"strings"
)

// Checkboxes must start at column 0; nested (indented) ones are deliberately not counted.
var (
	taskRe          = regexp.MustCompile(`(?i)^[-*]\s+\[([\sx])\]\s*(.*)$`)
	completedTaskRe = regexp.MustCompile(`(?i)^[-*]\s+\[x\]`)
)

// TaskProgress is the checkbox tally for a tasks file.
type TaskProgress struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
}

// Task is a single checkbox line.
type Task struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
}

// CountTasks tallies checkboxes in tasks-file content.
func CountTasks(content string) TaskProgress {
	var p TaskProgress
	for _, line := range strings.Split(content, "\n") {
		if taskRe.MatchString(line) {
			p.Total++
			if completedTaskRe.MatchString(line) {
				p.Completed++
			}
		}
	}
	return p
}

// ParseTasks returns the checkbox lines in order.
func ParseTasks(content string) []Task {
	var tasks []Task
	for _, line := range strings.Split(content, "\n") {
		m := taskRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		tasks = append(tasks, Task{
			Description: strings.TrimSpace(m[2]),
			Done:        strings.EqualFold(m[1], "x"),
		})
	}
	return tasks
}

// FormatTaskStatus renders progress the way list output shows it.
func FormatTaskStatus(p TaskProgress) string {
	if p.Total == 0 {
		return "No tasks"
	}
	if p.Completed == p.Total {
		return "✓ Complete"
	}
	return strconv.Itoa(p.Completed) + "/" + strconv.Itoa(p.Total) + " tasks"
}
