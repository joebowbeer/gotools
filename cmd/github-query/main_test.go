package main

import (
	"testing"
	"time"
)

func TestValidateOptions(t *testing.T) {
	tests := []struct {
		name    string
		listOpt string
		orgOpt  string
		repoOpt string
		valid   bool
	}{
		{"1", "", "", "", false},
		{"2", "", "myOrg", "myRepo", false},
		{"3", "repos", "", "", false},
		{"4", "repos", "myOrg", "", true},
		{"5", "repos", "myOrg", "myRepo", false},
		{"6", "commits", "", "", false},
		{"7", "commits", "myOrg", "", false},
		{"8", "commits", "myOrg", "myRepo", true},
		{"9", "pull-requests", "", "myRepo", false},
		{"10", "pull-requests", "myOrg", "myRepo", true},
		{"11", "foobar", "", "", false},
		{"12", "foobar", "myOrg", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateOptions(tt.listOpt, tt.orgOpt, tt.repoOpt); err == nil != tt.valid {
				t.Errorf("validateOptions err=[%v], wanted valid=%v", err, tt.valid)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	now := time.Now()
	jul := time.Date(2018, 7, 1, 0, 0, 0, 0, time.UTC)
	jan := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		timestr string
		timedef time.Time
		parsed  time.Time
	}{
		{"1", "", now, now},
		{"2", "2018-07-01", now, jul},
		{"3", "2018-07-01", jan, jul},
		{"4", "2019-01-01", jul, jan},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := parseDate(tt.timestr, tt.timedef)
			if v != tt.parsed {
				t.Errorf("parseDate got %v, wanted %v", v, tt.parsed)
			}
		})
	}
}
