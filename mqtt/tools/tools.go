package tools

import (
  "io"
)

type UnblockedWriter chan []byte

func Unblock(wc io.WriteCloser) (io.WriteCloser) {

	uw := make(UnblockedWriter)
  go func() {
    for b := range uw {
      wc.Write(b)
      //TODO catch and report errors
    }
    wc.Close()
  }()
  return uw
}

func (uw UnblockedWriter) Write(p []byte) (n int, err error) {

  uw <- p
  return len(p), nil
}

func (uw UnblockedWriter) Close() error {

  close(uw)
  return nil
}
