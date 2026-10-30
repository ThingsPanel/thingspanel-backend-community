package devicemetadata

import (
	"net/url"
	"testing"
)

func TestDecodeNativeStatusHistoryQuery(t *testing.T) {
	parse := func(raw string) (nativeStatusHistoryQuery, error) {
		values, err := url.ParseQuery(raw)
		if err != nil {
			t.Fatal(err)
		}
		return decodeNativeStatusHistoryQuery(values)
	}
	q, err := parse("from=2026-10-28T00:00:00Z&connectivity=online&page=2&pageSize=10")
	if err != nil || q.fromSec == nil || *q.fromSec != 1793145600 || q.online == nil || !*q.online || q.page != 2 {
		t.Fatalf("%+v %v", q, err)
	}
	if q, err := parse("connectivity=unknown"); err != nil || !q.unknownOnly {
		t.Fatalf("%+v %v", q, err)
	}
	if q, err := parse(""); err != nil || q.fromSec != nil || q.online != nil || q.page != 1 || q.pageSize != 20 {
		t.Fatalf("%+v %v", q, err)
	}
	for _, bad := range []string{"to=2026-10-28T00:00:00Z", "from=2026-10-28T00:00:00.5Z", "from=nope", "connectivity=bogus", "pageSize=0"} {
		if _, err := parse(bad); err == nil {
			t.Fatalf("%q must be refused", bad)
		}
	}
}
