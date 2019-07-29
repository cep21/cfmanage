package aimd

import "time"

// Aimd stands for https://en.wikipedia.org/wiki/Additive_increase/multiplicative_decrease
type Aimd struct {
	Max             time.Duration
	Min             time.Duration
	MultiplyByOnErr int64
	SubtractOnOk    time.Duration

	currentTime time.Duration
}

func (a *Aimd) max() time.Duration {
	if a.Max == 0 {
		return a.min() * 128
	}
	return a.Max
}

func (a *Aimd) min() time.Duration {
	if a.Min == 0 {
		return time.Second
	}
	return a.Min
}

func (a *Aimd) multiplyByOnErr() int64 {
	if a.MultiplyByOnErr == 0 {
		return 2
	}
	return a.MultiplyByOnErr
}

func (a *Aimd) subtractOnOk() time.Duration {
	if a.SubtractOnOk == 0 {
		return a.min() / 4
	}
	return a.SubtractOnOk
}

func (a *Aimd) Get() time.Duration {
	if a.currentTime == 0 {
		return a.min()
	}
	return a.currentTime
}

func (a *Aimd) OnError() {
	a.currentTime = time.Duration(a.currentTime.Nanoseconds() * a.multiplyByOnErr())
	a.boundCurrentTime()
}

func (a *Aimd) boundCurrentTime() {
	if a.currentTime < a.min() {
		a.currentTime = a.min()
	}
	if a.currentTime > a.max() {
		a.currentTime = a.max()
	}
}

func (a *Aimd) OnOk() {
	a.currentTime -= a.subtractOnOk()
	a.boundCurrentTime()
}
