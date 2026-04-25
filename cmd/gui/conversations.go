package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type ConversationInfo struct {
	Filename     string
	Path         string
	Date         time.Time
	UserMessages int
	AgentMessages int
	Lines        int
	Size         int64
}

func ListConversations(rootPath string) ([]ConversationInfo, error) {
	dir := filepath.Join(rootPath, ".lite-dev-agent", "conversations")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var convs []ConversationInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".txt") {
			continue
		}

		fullPath := filepath.Join(dir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		ci := ConversationInfo{
			Filename: entry.Name(),
			Path:     fullPath,
			Size:     info.Size(),
		}

		name := strings.TrimSuffix(entry.Name(), ".txt")
		if t, err := time.Parse("2006-01-02_15-04-05", name); err == nil {
			ci.Date = t
		}

		ci.UserMessages, ci.AgentMessages, ci.Lines = countMessages(fullPath)

		convs = append(convs, ci)
	}

	sort.Slice(convs, func(i, j int) bool {
		return convs[i].Date.After(convs[j].Date)
	})

	return convs, nil
}

func countMessages(path string) (userMsgs, agentMsgs, lines int) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		lines++
		if len(line) > 21 && line[0] == '#' && line[1] == '!' {
			rest := line[20:]
			if strings.Contains(rest, "| user_message") {
				userMsgs++
			} else if strings.Contains(rest, "| agent_response") {
				agentMsgs++
			}
		}
	}
	return
}

func FormatConversationDate(ci ConversationInfo) string {
	if ci.Date.IsZero() {
		return ci.Filename
	}
	return ci.Date.Format("Mon, Jan 2 2006 15:04")
}

func FormatConversationSummary(ci ConversationInfo) string {
	date := FormatConversationDate(ci)
	total := ci.UserMessages + ci.AgentMessages
	return fmt.Sprintf("%s  |  %d messages (%d user, %d agent)  |  %d lines  |  %s",
		date, total, ci.UserMessages, ci.AgentMessages, ci.Lines, formatSize(ci.Size))
}

func formatSize(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}
	if size < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
}
