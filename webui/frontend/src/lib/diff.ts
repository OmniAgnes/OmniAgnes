// 轻量行级 diff：用于把工具调用（如 edit 的 old_string/new_string）渲染成带 +/- 的差异视图。
// 采用 LCS 动态规划，文本片段通常较短，O(n*m) 完全够用。

export type DiffOp = "keep" | "add" | "del"

export interface DiffLine {
  op: DiffOp
  text: string
  /** 原文件行号（keep/del），从 1 开始 */
  oldNo?: number
  /** 新文件行号（keep/add），从 1 开始 */
  newNo?: number
}

/** 计算两段文本的行级差异。 */
export function diffLines(oldText: string, newText: string): DiffLine[] {
  const a = splitLines(oldText)
  const b = splitLines(newText)

  // lcs[i][j] = a[i..] 与 b[j..] 的最长公共子序列长度
  const n = a.length
  const m = b.length
  const lcs: number[][] = Array.from({ length: n + 1 }, () => new Array<number>(m + 1).fill(0))
  for (let i = n - 1; i >= 0; i--) {
    for (let j = m - 1; j >= 0; j--) {
      lcs[i][j] = a[i] === b[j] ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1])
    }
  }

  const out: DiffLine[] = []
  let i = 0
  let j = 0
  let oldNo = 1
  let newNo = 1
  while (i < n && j < m) {
    if (a[i] === b[j]) {
      out.push({ op: "keep", text: a[i], oldNo: oldNo++, newNo: newNo++ })
      i++
      j++
    } else if (lcs[i + 1][j] >= lcs[i][j + 1]) {
      out.push({ op: "del", text: a[i], oldNo: oldNo++ })
      i++
    } else {
      out.push({ op: "add", text: b[j], newNo: newNo++ })
      j++
    }
  }
  while (i < n) out.push({ op: "del", text: a[i++], oldNo: oldNo++ })
  while (j < m) out.push({ op: "add", text: b[j++], newNo: newNo++ })
  return out
}

function splitLines(s: string): string[] {
  if (s === "") return []
  return s.replace(/\r\n/g, "\n").split("\n")
}

/** 统计差异行数，用于折叠提示。 */
export function diffStat(lines: DiffLine[]): { added: number; removed: number } {
  let added = 0
  let removed = 0
  for (const l of lines) {
    if (l.op === "add") added++
    else if (l.op === "del") removed++
  }
  return { added, removed }
}
