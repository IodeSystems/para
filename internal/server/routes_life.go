package server

import "github.com/iodesystems/parasolve/internal/ops"

// registerLife: areas, notes, links, and dependency edits. Reads are GET with
// vault:read, writes POST with vault:write.
func registerLife(r *registrar) {
	reg(r, ops.AreaAdd, post, write)
	reg(r, ops.AreaList, get, read)
	reg(r, ops.AreaShow, get, read)
	reg(r, ops.NoteAdd, post, write)
	reg(r, ops.NoteList, get, read)
	reg(r, ops.NoteShow, get, read)
	reg(r, ops.TaskDue, post, write)
}
