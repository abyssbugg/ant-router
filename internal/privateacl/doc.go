// Package privateacl builds and verifies Windows access control lists that
// make a file or directory private to the current user: a protected DACL
// (nothing inherited from the parent) granting full access only to SYSTEM
// and the current user. It is the Windows counterpart of mode 0600/0700;
// permission bits do not restrict access on Windows. The package has no
// API on other platforms.
package privateacl
