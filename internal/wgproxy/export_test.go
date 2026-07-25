package wgproxy

import "time"

// SetExchangeTimeoutForTest shrinks the Exchange timeout so the timeout path
// is testable without waiting the production default. Call before Exchange;
// not safe concurrently with an in-flight Exchange.
func (p *Proxy) SetExchangeTimeoutForTest(d time.Duration) {
	p.exchangeTimeout = d
}

// NoteTruncationForTest exposes the truncation tripwire: a real UDP datagram
// cannot exceed 65507 payload bytes, so n == len(buf) with a 65535-byte
// buffer is not reachable over a real socket in tests.
func (p *Proxy) NoteTruncationForTest(n, bufLen int) {
	p.noteTruncation(n, bufLen)
}
