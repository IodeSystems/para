// Dates the Later button offers.

const day = (d: Date) => {
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

// laterDates are the dates Later offers, in local time: tomorrow, and next week (the Monday after
// today).
export function laterDates(now: Date): { tomorrow: string; nextWeek: string } {
  const t = new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1)
  const ahead = (8 - now.getDay()) % 7 || 7
  const w = new Date(now.getFullYear(), now.getMonth(), now.getDate() + ahead)
  return { tomorrow: day(t), nextWeek: day(w) }
}
