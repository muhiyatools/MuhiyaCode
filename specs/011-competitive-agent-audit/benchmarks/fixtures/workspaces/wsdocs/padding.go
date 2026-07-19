package wsdocs

// PadLeft left-pads s with pad until it reaches width.
func PadLeft(s, pad string, width int) string {
  if pad == "" {
      return s
  }
  for len(s) < width {
	s = pad + s
    }
	return s
}
