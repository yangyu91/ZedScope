module yamiua

go 1.22

// yami-UA core is standard-library only except for the optional SSH session
// type (yamiua/ai/ssh), which is backed by golang.org/x/crypto/ssh.

require (
	golang.org/x/crypto v0.31.0
	golang.org/x/sys v0.47.0 // indirect
)
