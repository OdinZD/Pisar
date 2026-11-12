// Where the search panel sits in the editor column, as a pure function.

/**
 * Distance in px from the bottom of the editor column to the top of Neovim's status line, i.e. how
 * far up the panel's bottom edge has to be. The terminal grid is `rows` cells high and starts at the
 * top of the column; fitting leaves a remainder of less than one cell below it, which belongs to the
 * inset as well. The last `reservedRows` rows (status line, command line) stay uncovered.
 * Anything that cannot be measured gives 0: the panel then sits at the very bottom.
 */
export function statusInset(containerHeight: number, rows: number, cellHeight: number, reservedRows: number): number {
  if (![containerHeight, rows, cellHeight, reservedRows].every(Number.isFinite) || cellHeight <= 0 || rows < 1) return 0
  const coveredRows = Math.max(0, rows - reservedRows)
  return Math.max(0, containerHeight - coveredRows * cellHeight)
}
