package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/asm"
	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/rlimit"
)

type addrList []*net.UDPAddr

func (a *addrList) String() string {
	parts := make([]string, len(*a))
	for i, addr := range *a {
		parts[i] = addr.String()
	}
	return strings.Join(parts, ",")
}

func (a *addrList) Set(s string) error {
	addr, err := net.ResolveUDPAddr("udp4", s)
	if err != nil {
		return err
	}
	*a = append(*a, addr)
	return nil
}

func main() {
	var dsts addrList
	flag.Var(&dsts, "dst", "multicast destination group:port to match (repeatable)")
	src := flag.String("src", "", "source IP to match (required)")
	rewriteSrc := flag.String("rewrite-src", "", "rewrite source IP to this value (required)")
	flag.Parse()

	if *src == "" || *rewriteSrc == "" {
		flag.Usage()
		log.Fatalf("both -src and -rewrite-src are required")
	}
	if len(dsts) == 0 {
		flag.Usage()
		log.Fatalf("at least one -dst is required")
	}

	origIP := net.ParseIP(*src).To4()
	newIP := net.ParseIP(*rewriteSrc).To4()
	if origIP == nil || newIP == nil {
		log.Fatalf("invalid IPs: src=%q rewrite-src=%q", *src, *rewriteSrc)
	}

	detach, err := attachSrcRewrite(origIP, newIP, dsts)
	if err != nil {
		log.Fatalf("attach eBPF: %v", err)
	}
	defer func() {
		log.Println("detaching eBPF program")
		detach()
	}()

	log.Printf("rewriting src %s -> %s for %d destination(s): %s", origIP, newIP, len(dsts), dsts.String())
	log.Printf("waiting for signal to detach...")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("shutting down")
}

// attachSrcRewrite loads and attaches a BPF_CGROUP_UDP4_SENDMSG program that
// rewrites the source IP from origIP to newIP when the destination matches
// any of the provided multicast address/port pairs. Destination matching is
// inlined as a chain of comparisons. Attaches to the root cgroup.
// Returns a cleanup function.
//
// struct bpf_sock_addr layout (from /usr/include/linux/bpf.h):
//
//	offset 0:  user_family   __u32
//	offset 4:  user_ip4      __u32   ← destination IP (network order)
//	offset 8:  user_ip6[4]   __u32×4
//	offset 24: user_port     __u32   ← destination port (htons, zero-extended)
//	offset 28: family        __u32
//	offset 32: type          __u32
//	offset 36: protocol      __u32
//	offset 40: msg_src_ip4   __u32   ← source IP (network order)
func attachSrcRewrite(origIP, newIP net.IP, dsts addrList) (func(), error) {
	// Best-effort: on kernels >= 5.11, BPF memory uses cgroup accounting
	// and memlock is irrelevant. If we lack CAP_SYS_RESOURCE, just proceed.
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Printf("warning: failed to remove memlock rlimit (may be fine on kernel >= 5.11): %v", err)
	}

	origU32 := binary.NativeEndian.Uint32(origIP)
	newU32 := binary.NativeEndian.Uint32(newIP)

	// BPF program:
	//   if (ctx->msg_src_ip4 != origIP) goto end;
	//   if (!is_multicast(ctx->user_ip4)) goto end;
	//   dst_ip = ctx->user_ip4; dst_port = ctx->user_port;
	//   if (dst_ip == dst1_ip && dst_port == dst1_port) goto rewrite;
	//   if (dst_ip == dst2_ip && dst_port == dst2_port) goto rewrite;
	//   ...
	//   goto end;
	//   rewrite: ctx->msg_src_ip4 = newIP;
	//   end: return 1;
	insns := asm.Instructions{
		// r6 = ctx
		asm.Mov.Reg(asm.R6, asm.R1),

		// --- Check source IP ---
		asm.LoadMem(asm.R0, asm.R6, 40, asm.Word),
		asm.Mov.Imm(asm.R1, int32(origU32)),
		asm.LSh.Imm(asm.R1, 32),
		asm.RSh.Imm(asm.R1, 32),
		asm.JNE.Reg(asm.R0, asm.R1, "end"),

		// --- Check destination is multicast ---
		asm.LoadMem(asm.R7, asm.R6, 4, asm.Word), // r7 = dst IP (kept for comparisons)
		asm.Mov.Reg(asm.R0, asm.R7),
		asm.And.Imm(asm.R0, 0xF0),
		asm.JNE.Imm(asm.R0, 0xE0, "end"),

		// r8 = dst port (kept for comparisons)
		asm.LoadMem(asm.R8, asm.R6, 24, asm.Word),
	}

	// --- Inline destination (IP, port) comparisons ---
	for _, dst := range dsts {
		ip := dst.IP.To4()
		portBuf := make([]byte, 2)
		binary.BigEndian.PutUint16(portBuf, uint16(dst.Port))

		dstIPU32 := binary.NativeEndian.Uint32(ip)
		dstPortVal := int32(binary.NativeEndian.Uint16(portBuf))

		// if (r7 == dst_ip && r8 == dst_port) goto rewrite
		// Use r1 for the IP comparison (zero-extended via shift).
		insns = append(insns,
			asm.Mov.Imm(asm.R1, int32(dstIPU32)),
			asm.LSh.Imm(asm.R1, 32),
			asm.RSh.Imm(asm.R1, 32),
			asm.JNE.Reg(asm.R7, asm.R1, "next_"+dst.String()),
			asm.JEq.Imm(asm.R8, dstPortVal, "rewrite"),
			asm.Mov.Imm(asm.R0, 0).WithSymbol("next_"+dst.String()), // nop landing pad
		)
	}

	// No destination matched — fall through to end.
	insns = append(insns, asm.Ja.Label("end"))

	// rewrite: ctx->msg_src_ip4 = newIP
	insns = append(insns,
		asm.StoreImm(asm.R6, 40, int64(newU32), asm.Word).WithSymbol("rewrite"),
	)

	// end: return 1 (allow)
	insns = append(insns,
		asm.Mov.Imm(asm.R0, 1).WithSymbol("end"),
		asm.Return(),
	)

	spec := &ebpf.ProgramSpec{
		Type:         ebpf.CGroupSockAddr,
		AttachType:   ebpf.AttachCGroupUDP4Sendmsg,
		Instructions: insns,
		License:      "GPL",
	}

	prog, err := ebpf.NewProgram(spec)
	if err != nil {
		return nil, fmt.Errorf("load program: %w", err)
	}

	cgroupPath := "/sys/fs/cgroup"
	l, err := link.AttachCgroup(link.CgroupOptions{
		Path:    cgroupPath,
		Attach:  ebpf.AttachCGroupUDP4Sendmsg,
		Program: prog,
	})
	if err != nil {
		prog.Close()
		return nil, fmt.Errorf("attach to cgroup %s: %w", cgroupPath, err)
	}

	log.Printf("eBPF program attached to %s (fd=%d)", cgroupPath, prog.FD())

	cleanup := func() {
		l.Close()
		prog.Close()
	}
	return cleanup, nil
}
