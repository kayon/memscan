package memscan

import "gihub.com/kayon/memscan/utils"

type Permissions uint8

const (
	PermRead Permissions = 1 << iota
	PermWrite
	PermExec
	PermPrivate
	PermShared
)

func (p Permissions) String() string {
	res := []byte("----")
	if p.Read() {
		res[0] = 'r'
	}
	if p.Write() {
		res[1] = 'w'
	}
	if p.Exec() {
		res[2] = 'x'
	}

	if p.Private() {
		res[3] = 'p'
	} else if p.Shared() {
		res[3] = 's'
	}

	return utils.BytesToString(res)
}

func (p Permissions) Read() bool {
	return p&PermRead != 0
}

func (p Permissions) Write() bool {
	return p&PermWrite != 0
}

func (p Permissions) Exec() bool {
	return p&PermExec != 0
}

func (p Permissions) Private() bool {
	return p&PermPrivate != 0
}

func (p Permissions) Shared() bool {
	return p&PermShared != 0
}

func ParsePermissions[T string | []byte](s T) (p Permissions) {
	if len(s) < 4 {
		return
	}
	if s[0] == 'r' {
		p |= PermRead
	}
	if s[1] == 'w' {
		p |= PermWrite
	}
	if s[2] == 'x' {
		p |= PermExec
	}
	if s[3] == 'p' {
		p |= PermPrivate
	} else if s[3] == 's' {
		p |= PermShared
	}
	return p
}
