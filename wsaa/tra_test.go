package wsaa

import (
	"fmt"
	"regexp"
	"strconv"
	"testing"
	"time"
)

// traRe follows loginTicketRequest.xsd: header (uniqueId, generationTime,
// expirationTime, in that order; source and destination are optional and not
// sent) and then service.
var traRe = regexp.MustCompile(`^<\?xml version="1\.0" encoding="UTF-8"\?>\n` +
	`<loginTicketRequest version="1\.0"><header>` +
	`<uniqueId>(\d+)</uniqueId>` +
	`<generationTime>([^<]+)</generationTime>` +
	`<expirationTime>([^<]+)</expirationTime>` +
	`</header><service>([^<]+)</service></loginTicketRequest>$`)

// checkTRA validates a TRA the way WSAA does (answering xml.bad otherwise):
// the schema order, uniqueId as xs:unsignedInt (32 bits) and both times as
// xs:dateTime with generationTime before expirationTime.
func checkTRA(tra []byte) error {
	m := traRe.FindSubmatch(tra)
	if m == nil {
		return fmt.Errorf("does not follow loginTicketRequest.xsd: %s", tra)
	}
	if _, err := strconv.ParseUint(string(m[1]), 10, 32); err != nil {
		return fmt.Errorf("uniqueId %s is not an xs:unsignedInt: %w", m[1], err)
	}
	gen, err := time.Parse(time.RFC3339, string(m[2]))
	if err != nil {
		return fmt.Errorf("generationTime: %w", err)
	}
	exp, err := time.Parse(time.RFC3339, string(m[3]))
	if err != nil {
		return fmt.Errorf("expirationTime: %w", err)
	}
	if !gen.Before(exp) {
		return fmt.Errorf("generationTime %s is not before expirationTime %s", gen, exp)
	}
	return nil
}

func TestBuildTRA_FollowsSchema(t *testing.T) {
	now := time.Now()
	tra, err := buildTRA(now, "wsfe")
	if err != nil {
		t.Fatal(err)
	}
	if err := checkTRA(tra); err != nil {
		t.Fatal(err)
	}
	m := traRe.FindSubmatch(tra)
	if string(m[4]) != "wsfe" {
		t.Errorf("service = %s", m[4])
	}
	gen, _ := time.Parse(time.RFC3339, string(m[2]))
	if gen.After(now) {
		t.Errorf("generationTime %s must not be in the future", gen)
	}
}

// TestNextUniqueID_FitsAndIsUnique verifies uniqueId fits in 32 bits (the
// previous UnixNano value made WSAA answer xml.bad) and that two TRAs in the
// same second get different ids.
func TestNextUniqueID_FitsAndIsUnique(t *testing.T) {
	now := time.Now()
	a, b := nextUniqueID(now), nextUniqueID(now)
	if a == b {
		t.Fatalf("two TRAs in the same second share uniqueId %d", a)
	}
	if uint64(a) < uint64(now.Unix()) {
		t.Errorf("uniqueId %d should be at least the Unix time %d", a, now.Unix())
	}
}

// TestCheckTRA_RejectsUnixNanoUniqueID pins the fake WSAA check: the uniqueId
// sent up to v0.5.1 (time.UnixNano) does not fit xs:unsignedInt.
func TestCheckTRA_RejectsUnixNanoUniqueID(t *testing.T) {
	now := time.Now()
	tra := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+"\n"+
		`<loginTicketRequest version="1.0"><header><uniqueId>%d</uniqueId>`+
		`<generationTime>%s</generationTime><expirationTime>%s</expirationTime>`+
		`</header><service>wsfe</service></loginTicketRequest>`,
		now.UnixNano(), now.Add(-time.Minute).Format(time.RFC3339), now.Add(time.Minute).Format(time.RFC3339))
	if err := checkTRA([]byte(tra)); err == nil {
		t.Fatal("a UnixNano uniqueId must be rejected")
	}
}
