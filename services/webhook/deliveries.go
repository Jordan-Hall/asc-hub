// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"

	webhook_model "forgejo.org/models/webhook"
	"forgejo.org/modules/json"
)

// deliveryLines is how many lines of a request or response body the delivery detail shows.
const deliveryLines = 400

// Delivery is one task of a webhook as its Recent deliveries view shows it.
type Delivery struct {
	*webhook_model.HookTask
}

// Deliveries wraps tasks for the Recent deliveries view.
func Deliveries(tasks []*webhook_model.HookTask) []*Delivery {
	rows := make([]*Delivery, len(tasks))
	for i, t := range tasks {
		rows[i] = &Delivery{t}
	}
	return rows
}

// ShortID is the first part of the delivery's UUID, enough to tell deliveries apart.
func (d *Delivery) ShortID() string {
	id, _, _ := strings.Cut(d.UUID, "-")
	return id
}

// Event names what the delivery was about, as "release published": the event type, and the action when the
// payload has one.
func (d *Delivery) Event() string {
	event := strings.ReplaceAll(string(d.EventType), "_", " ")
	var payload struct {
		Action string `json:"action"`
	}
	if json.Unmarshal([]byte(d.PayloadContent), &payload) == nil && payload.Action != "" {
		return strings.TrimSpace(event + " " + strings.ReplaceAll(payload.Action, "_", " "))
	}
	return event
}

// Status is the HTTP status the receiver answered, 0 when it did not answer or the task is waiting.
func (d *Delivery) Status() int {
	if d.ResponseInfo == nil {
		return 0
	}
	return d.ResponseInfo.Status
}

// StatusLine is the status with its reason phrase, as "502 Bad Gateway".
func (d *Delivery) StatusLine() string {
	return strings.TrimSpace(fmt.Sprintf("%d %s", d.Status(), http.StatusText(d.Status())))
}

// Seconds is the duration in seconds with one decimal, for a duration of a second or more.
func (d *Delivery) Seconds() string {
	return fmt.Sprintf("%.1f", float64(d.DurationMS)/1000)
}

// Body is the request or response body, indented when it is JSON, cut to its first lines; More counts what was cut.
func (d *Delivery) Body(part string) (lines []string, more int) {
	var body string
	if part == "response" {
		if d.ResponseInfo != nil {
			body = d.ResponseInfo.Body
		}
	} else if d.RequestInfo != nil && d.RequestInfo.Body != "" {
		body = d.RequestInfo.Body
	} else {
		body = d.PayloadContent
	}
	var out bytes.Buffer
	if json.Indent(&out, []byte(body), "", "  ") == nil {
		body = out.String()
	}
	body = strings.TrimRight(body, "\n")
	if body == "" {
		return nil, 0
	}
	lines = strings.Split(body, "\n")
	if len(lines) > deliveryLines {
		return lines[:deliveryLines], len(lines) - deliveryLines
	}
	return lines, 0
}

// BodyLines is Body without the count of cut lines, for templates.
func (d *Delivery) BodyLines(part string) []string {
	lines, _ := d.Body(part)
	return lines
}

// BodyMore is how many lines of the body the detail does not show.
func (d *Delivery) BodyMore(part string) int {
	_, more := d.Body(part)
	return more
}
