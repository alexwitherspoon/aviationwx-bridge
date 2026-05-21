package upload

// Interrupter aborts an in-flight Upload by closing the underlying connection.
type Interrupter interface {
	Interrupt()
}

// InterruptUpload closes the active connection when c supports Interrupter.
func InterruptUpload(c Client) {
	if c == nil {
		return
	}
	if i, ok := c.(Interrupter); ok {
		i.Interrupt()
	}
}
