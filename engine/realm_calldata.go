package engine

// Layout: the linker places functions in file order, each aligned to 32
// bytes by default, and the interpreter's speed depends on the 64-byte
// phase of (*Realm).run (propGetMono moves by about 8 %). This file's name
// and ReleaseCallData's size, and the places of a few functions elsewhere
// in the package (escapeByte, proxy, sweepWeak, JobsPending, ownCap), were
// tuned to keep the hot functions' phase in the test binaries. That tuning
// is over: the gate's binaries link with -funcalign=64 (docs/DESIGN.md
// §14.2), where every function starts a 64-byte line whatever comes before
// it, so none of these sizes and places needs keeping.

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
// their own storage. It also starts a new memory budget (ResetAllocation).
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
		if lz.mem != nil {
			r.ResetAllocation()
		}
	}
}
