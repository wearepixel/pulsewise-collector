package main

import (
	"reflect"
	"testing"

	psnet "github.com/shirou/gopsutil/v3/net"
)

func conn(status string, pid int32, localPort uint32, remoteIP string, remotePort uint32) psnet.ConnectionStat {
	return psnet.ConnectionStat{
		Status: status,
		Pid:    pid,
		Laddr:  psnet.Addr{IP: "10.0.0.5", Port: localPort},
		Raddr:  psnet.Addr{IP: remoteIP, Port: remotePort},
	}
}

func TestStuckConnectionsNameTheProcessAndTheService(t *testing.T) {
	names := map[int32]string{10: "php-fpm8.4", 11: "php-fpm8.4", 20: "nginx"}
	conns := []psnet.ConnectionStat{
		conn("LISTEN", 20, 443, "", 0),
		conn("ESTABLISHED", 10, 50001, "127.0.0.1", 6379),
		conn("CLOSE_WAIT", 10, 50002, "127.0.0.1", 6379),
		conn("CLOSE_WAIT", 11, 50003, "127.0.0.1", 6379),
		conn("CLOSE_WAIT", 20, 443, "203.0.113.9", 61000),
	}

	got := stuckConnections(conns, func(pid int32) string { return names[pid] })
	want := []StuckConnection{
		{Process: "php-fpm8.4", Direction: "outbound", Address: "127.0.0.1", Port: 6379, Count: 2},
		{Process: "nginx", Direction: "inbound", Port: 443, Count: 1},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}

func TestStuckConnectionsAreCappedToTheLargestGroups(t *testing.T) {
	conns := []psnet.ConnectionStat{}
	for port := uint32(1); port <= maxStuckConnectionGroups+5; port++ {
		conns = append(conns, conn("CLOSE_WAIT", 1, 40000+port, "10.0.0.9", port))
	}
	conns = append(conns, conn("CLOSE_WAIT", 1, 49999, "10.0.0.9", 15))

	got := stuckConnections(conns, func(int32) string { return "worker" })
	if len(got) != maxStuckConnectionGroups {
		t.Fatalf("got %d groups", len(got))
	}
	if got[0].Port != 15 || got[0].Count != 2 {
		t.Fatalf("largest group must come first, got %+v", got[0])
	}
}

func TestNoStuckConnectionsReportsNothing(t *testing.T) {
	got := stuckConnections([]psnet.ConnectionStat{conn("ESTABLISHED", 1, 50000, "10.0.0.9", 3306)}, func(int32) string { return "x" })
	if len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestClientAddressIsNotReportedWhenTheListenerHasGone(t *testing.T) {
	conns := []psnet.ConnectionStat{conn("CLOSE_WAIT", 20, 443, "203.0.113.9", 61000)}

	got := stuckConnections(conns, func(int32) string { return "nginx" })
	want := []StuckConnection{{Process: "nginx", Direction: "inbound", Port: 443, Count: 1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
}
