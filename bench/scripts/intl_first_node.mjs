// Times the first use of Intl in a fresh Node.js process, as
// bench/cmd/intlfirst does in moejs: it compiles the script given as its
// argument, runs it once and prints how long the run took, in microseconds:
//
//	TZ=UTC node bench/scripts/intl_first_node.mjs '(1234.5).toLocaleString()'
const f = new Function("return " + process.argv[2]);
const t0 = performance.now();
const v = f();
const us = (performance.now() - t0) * 1000;
console.log(`${us.toFixed(1)} us\t${v}`);
