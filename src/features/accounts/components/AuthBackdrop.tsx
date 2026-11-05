// What sits behind the sign-in screens: a sparse network of nodes and links, as faint as the logo's orange can be and still be seen, with a soft glow
// behind the mark. It echoes the product (a topology) and the mark (a path with nodes) and asks for nothing: it is the same picture every time (the
// points come from a fixed seed, so a screenshot or a test never changes), it draws from the theme's accent so both themes have one, and it fades out
// toward the corners so the form is never read against a line.

const W = 1600
const H = 1000
const COLS = 11
const ROWS = 7

/** A small seeded generator: the same numbers on every load. */
function mulberry32(seed: number) {
  let a = seed
  return () => {
    a = (a + 0x6d2b79f5) | 0
    let t = Math.imul(a ^ (a >>> 15), 1 | a)
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296
  }
}

function network() {
  const rnd = mulberry32(7)
  const between = (lo: number, hi: number) => lo + rnd() * (hi - lo)
  const points: { x: number; y: number; r: number }[] = []
  for (let i = 0; i < COLS; i++) {
    for (let j = 0; j < ROWS; j++) {
      const x = (i + 0.5 + between(-0.38, 0.38)) * (W / COLS)
      const y = (j + 0.5 + between(-0.38, 0.38)) * (H / ROWS)
      const keep = rnd() < 0.82
      const r = [2, 2.5, 3, 4][Math.floor(rnd() * 4)]
      if (keep) points.push({ x: Math.round(x), y: Math.round(y), r })
    }
  }
  const links = new Set<string>()
  points.forEach((p, a) => {
    const near = points
      .map((q, b) => ({ b, d: Math.hypot(p.x - q.x, p.y - q.y) }))
      .filter((n) => n.b !== a)
      .sort((m, n) => m.d - n.d)
      .slice(0, 3)
    const take = [1, 2, 2][Math.floor(rnd() * 3)]
    for (const n of near.slice(0, take)) if (n.d < 260) links.add(a < n.b ? `${a}-${n.b}` : `${n.b}-${a}`)
  })
  return { points, links: [...links].map((k) => k.split('-').map(Number) as [number, number]) }
}

const NETWORK = network()

const FADE = 'radial-gradient(75% 75% at 50% 45%, #000 25%, transparent 100%)'

export default function AuthBackdrop() {
  return (
    <div className="pointer-events-none absolute inset-0" aria-hidden data-testid="auth-backdrop">
      <div className="absolute inset-0" style={{ background: 'radial-gradient(60% 55% at 50% 38%, color-mix(in srgb, var(--color-accent) var(--auth-glow, 16%), transparent), transparent 70%)' }} />
      <svg className="absolute inset-0 h-full w-full" viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="xMidYMid slice" style={{ WebkitMaskImage: FADE, maskImage: FADE }}>
        <g stroke="var(--color-accent)" strokeOpacity="var(--auth-line, 0.16)" strokeWidth="1">
          {NETWORK.links.map(([a, b]) => (
            <line key={`${a}-${b}`} x1={NETWORK.points[a].x} y1={NETWORK.points[a].y} x2={NETWORK.points[b].x} y2={NETWORK.points[b].y} />
          ))}
        </g>
        <g fill="var(--color-accent)" fillOpacity="var(--auth-node, 0.35)">
          {NETWORK.points.map((p, i) => (
            <circle key={i} cx={p.x} cy={p.y} r={p.r} />
          ))}
        </g>
      </svg>
    </div>
  )
}
