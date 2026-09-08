package codexownership

// SecureDirectory applies Ant Router's private ownership-directory policy.
func SecureDirectory(path string) error { return secureDirectory(path) }

// SecureFile applies Ant Router's private ownership-file policy.
func SecureFile(path string) error { return secureFile(path) }
