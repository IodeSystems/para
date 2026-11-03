package model

// receiptFence opens and closes the block of a receipt body. Text from a
// document is always indented four or more spaces in the YAML, and a fence
// closes only at three or fewer, so no text can close the block.
const receiptFence = "````"
