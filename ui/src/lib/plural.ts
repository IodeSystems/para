// plural writes a count with its noun: "1 stall", "2 stalls".
export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`
}
