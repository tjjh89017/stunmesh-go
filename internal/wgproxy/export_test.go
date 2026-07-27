package wgproxy

import "time"

// SetExchangeTimeoutForTest shrinks the Exchange timeout; not safe
// concurrently with an in-flight Exchange.
func (p *Proxy) SetExchangeTimeoutForTest(d time.Duration) {
	p.exchangeTimeout = d
}

// NoteTruncationForTest exposes the truncation tripwire, unreachable over a
// real socket (UDP payload max 65507 < the 65535-byte buffer).
func (p *Proxy) NoteTruncationForTest(n, bufLen int) {
	p.noteTruncation(n, bufLen)
}
