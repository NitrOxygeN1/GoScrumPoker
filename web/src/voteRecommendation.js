/** Numeric card values in sort order, for "nearest to mean" (excludes ? and coffee). */
const DEFAULT_NUMS = [1, 2, 3, 5, 8, 13];

/** Deck order for tie display when values are not both plain numbers. */
const DECK_TIE_ORDER = ["1", "2", "3", "5", "8", "13", "?", "coffee"];

function compareLeaderValues(a, b) {
  const na = a === "?" || a === "coffee" ? NaN : parseFloat(a);
  const nb = b === "?" || b === "coffee" ? NaN : parseFloat(b);
  if (!Number.isNaN(na) && !Number.isNaN(nb)) {
    return na - nb;
  }
  return DECK_TIE_ORDER.indexOf(a) - DECK_TIE_ORDER.indexOf(b);
}

/**
 * @param {Record<string, string> | undefined} votes
 * @param {boolean} revealed
 * @param {{ format?(v: string) => string, numericOptions?: number[] }=} opt
 * @returns {{ line: string } | null} null = nothing to show
 */
export function computeVoteRecommendation(votes, revealed, opt = {}) {
  if (!revealed || !votes) return null;
  const all = Object.values(votes).filter((v) => v != null && String(v).trim() !== "");
  if (all.length === 0) return null;

  const format = opt.format || ((v) => (v === "coffee" ? "☕" : String(v)));
  const numOpts = (opt.numericOptions || DEFAULT_NUMS).filter((n) => !Number.isNaN(n));

  const byVal = new Map();
  for (const v of all) {
    byVal.set(v, (byVal.get(v) || 0) + 1);
  }
  if (byVal.size > 3) {
    return { line: "Revote: more than 3 different values" };
  }

  const T = all.length;
  const entries = Array.from(byVal.entries());
  const maxC = Math.max(...entries.map(([, c]) => c));
  const leaders = entries.filter(([, c]) => c === maxC).map(([v]) => v);

  if (leaders.length >= 3) {
    return { line: "Revote: 3+ options tied for the top" };
  }
  if (leaders.length === 2) {
    const [a, b] = [...leaders].sort(compareLeaderValues);
    return {
      line: `Tie between ${format(a)} & ${format(b)}`,
    };
  }

  const v = leaders[0];
  const c = byVal.get(v) ?? 0;
  if (c > T / 2) {
    return { line: `Recommend: ${format(v)}` };
  }

  const numVals = [];
  for (const x of all) {
    if (x === "?" || x === "coffee") continue;
    const n = parseFloat(x);
    if (!Number.isNaN(n)) numVals.push(n);
  }
  if (numVals.length === 0) {
    return { line: `Recommend: ${format(v)}` };
  }
  const mean = numVals.reduce((a, b) => a + b, 0) / numVals.length;
  let best = numOpts[0];
  let bestD = Math.abs(mean - best);
  for (const n of numOpts) {
    const d = Math.abs(mean - n);
    if (d < bestD) {
      bestD = d;
      best = n;
    }
  }
  return { line: `Recommend: ${format(String(best))}` };
}
