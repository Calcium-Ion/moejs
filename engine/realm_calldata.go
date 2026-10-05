package engine

// Layout: the linker places functions in file order, each aligned to 32
// bytes, and the interpreter's speed depends on the 64-byte phase of
// (*Realm).run (propGetMono moves by about 8 %). This file's name links
// ReleaseCallData after interp_run.go and object.go, so run, enterFrame,
// CallObject, callValue and lookupNamed keep dev's phase whatever its size.
// Its size, 997 bytes padded to 1024 (895 padded to 896, tuned by the
// order of the RegExp reset's two statements, before the reset of the last
// match), keeps the functions linked after it, the RegExp backtracker,
// (*String).flatten and the moejs package's (*Runtime).Call among them, in
// dev's phase too: its padded size may only change by a multiple of 64 (at
// 901 bytes they moved by 32). After changing this function, or the size
// of anything linked before run, build the bench test binary (go test -c
// in bench) on both sides and compare these functions' addresses in go
// tool nm -size -sort address, mod 64.

// Past these sizes ReleaseCallData lets a deep recursion's register stack
// and frame table go instead of clearing them; the next call allocates the
// initial sizes again.
const (
	maxRetainedStack  = 4096 // 64 KiB of registers
	maxRetainedFrames = 256  // 4 KiB
)

// ReleaseCallData drops the realm's references to the host values of
// finished calls, so a pooled realm does not keep the last request alive:
// the host conversion's chunks, whose placeholders hold the Go maps, slices
// and strings of the arguments (hostHeap); the registers and frame records
// the finished calls left above the live ones; the RegExp working copies
// of their subjects; and the last match, which the legacy RegExp statics
// (RegExp.$1, RegExp.input and the rest) describe: they read "" again, as
// in a new realm. Values already returned stay valid: their nodes keep
// their own storage.
//
// It ends the host conversion's period, which otherwise ends at the next
// top-level FromGo, so a host calls it where a request ends, after the
// request's last use of the realm (CallObject, ToGo, GetV, JSONStringify:
// each can run JavaScript): a request that runs several hooks on one realm
// is one period, whose usage sizes the next request's chunks. Calls do not
// do it themselves: clearing the register stack after each would tax every
// call, hosts that do not pool included, and split a request into periods.
//
// Between calls only; a no-op inside one (from a native function).
func (r *Realm) ReleaseCallData() {
	if r.callDepth != 0 {
		return
	}
	if h := r.fromGo.heap; h != nil {
		h.seal()
	}
	// No frame is live at depth 0, so sp and nframes are 0; the registers
	// and records below them would be kept all the same.
	st := &r.interp
	if st.sp == 0 && len(st.stack) > maxRetainedStack {
		st.stack = nil
	} else {
		clear(st.stack[st.sp:])
	}
	if st.nframes == 0 && len(st.frames) > maxRetainedFrames {
		st.frames = nil
	} else {
		clear(st.frames[st.nframes:])
	}
	if rs := r.regexps; rs != nil {
		rs.subjNext = 0
		clear(rs.subjects[:])
		rs.lastS, rs.lastC = nil, nil
	}
	if lz := r.lazy; lz != nil {
		lz.statics = nil
	}
}
