package app

import "golang.org/x/sys/windows/registry"

// scrollbarsAlways reports whether Windows's "Always show scrollbars" is
// on: DynamicScrollbars 0 under the user's accessibility settings. Absent,
// the scroll bars hide while the content is still.
func scrollbarsAlways() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Control Panel\Accessibility`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("DynamicScrollbars")
	return err == nil && v == 0
}
