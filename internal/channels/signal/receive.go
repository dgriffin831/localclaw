package signal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	receiveScanBufferBytes = 16 * 1024
	receiveScanMaxBytes    = 4 * 1024 * 1024
)

type ReceiveSettings struct {
	CLIPath            string
	Account            string
	Timeout            time.Duration
	MaxMessagesPerPoll int
	IgnoreAttachments  bool
	IgnoreStories      bool
}

type ReceiveMessage struct {
	Sender     string
	SenderName string
	Text       string
	Timestamp  int64
	IsGroup    bool
	GroupID    string
	GroupName  string
	IsSync     bool
}

type signalReceivePayload struct {
	Envelope signalReceiveEnvelope `json:"envelope"`
}

type signalReceiveEnvelope struct {
	Source       string                 `json:"source"`
	SourceNumber string                 `json:"sourceNumber"`
	SourceName   string                 `json:"sourceName"`
	Timestamp    int64                  `json:"timestamp"`
	SyncMessage  map[string]interface{} `json:"syncMessage"`
	DataMessage  signalDataMessage      `json:"dataMessage"`
}

type signalDataMessage struct {
	Message   string `json:"message"`
	GroupInfo struct {
		GroupID   string `json:"groupId"`
		GroupName string `json:"groupName"`
	} `json:"groupInfo"`
}

func ReceiveBatch(ctx context.Context, settings ReceiveSettings) ([]ReceiveMessage, error) {
	cliPath := strings.TrimSpace(settings.CLIPath)
	if cliPath == "" {
		cliPath = "signal-cli"
	}
	account := strings.TrimSpace(settings.Account)
	if account == "" {
		return nil, errors.New("signal receive account is required")
	}
	timeout := settings.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	maxMessages := settings.MaxMessagesPerPoll
	if maxMessages <= 0 {
		maxMessages = 10
	}
	timeoutSeconds := int(timeout / time.Second)
	if timeout%time.Second != 0 {
		timeoutSeconds++
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 1
	}

	args := []string{
		"-o", "json",
		"-a", account,
		"receive",
		"--timeout", strconv.Itoa(timeoutSeconds),
		"--max-messages", strconv.Itoa(maxMessages),
	}
	if settings.IgnoreAttachments {
		args = append(args, "--ignore-attachments")
	}
	if settings.IgnoreStories {
		args = append(args, "--ignore-stories")
	}

	cmd := exec.CommandContext(ctx, cliPath, args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrText := strings.TrimSpace(stderr.String())
		if ctx.Err() != nil {
			if stderrText == "" {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("signal receive canceled: %s: %w", stderrText, ctx.Err())
		}
		if stderrText == "" {
			return nil, fmt.Errorf("signal receive failed: %w", err)
		}
		return nil, fmt.Errorf("signal receive failed: %s: %w", stderrText, err)
	}

	return parseReceiveOutput(stdout.Bytes())
}

func parseReceiveOutput(output []byte) ([]ReceiveMessage, error) {
	messages := make([]ReceiveMessage, 0, 16)
	scanner := bufio.NewScanner(bytes.NewReader(output))
	scanner.Buffer(make([]byte, receiveScanBufferBytes), receiveScanMaxBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var payload signalReceivePayload
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return nil, fmt.Errorf("parse signal receive line: %w", err)
		}
		envelope := payload.Envelope
		sender := strings.TrimSpace(envelope.SourceNumber)
		if sender == "" {
			sender = strings.TrimSpace(envelope.Source)
		}
		groupID := strings.TrimSpace(envelope.DataMessage.GroupInfo.GroupID)
		groupName := strings.TrimSpace(envelope.DataMessage.GroupInfo.GroupName)
		messages = append(messages, ReceiveMessage{
			Sender:     sender,
			SenderName: strings.TrimSpace(envelope.SourceName),
			Text:       strings.TrimSpace(envelope.DataMessage.Message),
			Timestamp:  envelope.Timestamp,
			IsGroup:    groupID != "",
			GroupID:    groupID,
			GroupName:  groupName,
			IsSync:     len(envelope.SyncMessage) > 0,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan signal receive output: %w", err)
	}
	return messages, nil
}
