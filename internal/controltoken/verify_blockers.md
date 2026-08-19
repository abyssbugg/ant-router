# BLOCKER VERIFICATION CHECKLIST

## 1. 32-byte strict base64 token
- TokenSize = 32 bytes (token.go:11)
- RawURLEncoding.Strict() (token.go:15) - enforces strict decoding
- Parse validates length == EncodedTokenLength (43) (token.go:34)
- Verify re-encodes and compares (token.go:43-44) - canonical form check

## 2. Constant-time valid-token comparison
- Verify uses subtle.ConstantTimeCompare after Parse (token.go:60)
- Parse called first (lines 56-57), only then constant-time compare (line 60)

## 3. Bounded 44-byte file read
- io.LimitReader with EncodedTokenLength+1 = 44 bytes (store.go:123)
- Exact length check afterward (store.go:131: len(data) != EncodedTokenLength)

## 4. Temp+platform no-replace create convergence
- Unix: os.Link with os.IsExist check -> errTokenExists (permissions_unix.go:12-19)
- Windows: MoveFileExW with flag 0x8 (MOVEFILE_COPY_ALLOWED) (permissions_windows.go:22)
- installTokenFile error mapping (permissions_unix.go:14-15, permissions_windows.go:23-24)
- loadWinner calls loadPath on race winner (store.go:155-156)
- Race test: ConcurrentLoadOrCreateConverges (store_test.go:59-98)

## 5. Unix os.Link and Windows MoveFileExW no-replace flags/error mapping
- Unix installTokenFile: os.Link, os.IsExist -> errTokenExists (permissions_unix.go:12-19)
- Windows installTokenFile: MoveFileExW flag 0x8, ERROR_FILE_EXISTS/ERROR_ALREADY_EXISTS (permissions_windows.go:21-26)
- replaceTokenFile Unix: os.Rename (permissions_unix.go:21)
- replaceTokenFile Windows: MoveFileExW flags 0x1|0x8 (REPLACE_EXISTING | COPY_ALLOWED) (permissions_windows.go:29-30)

## 6. Atomic rotation and CodeDurabilityUncertain after post-replace sync failure
- writeTokenAtomicWithHooks sets committed=true after replaceTokenFile (rotate.go:52-55)
- Post-replace, syncOperation failure returns CodeDurabilityUncertain (rotate.go:60-61)
- Test: WriteTokenAtomicReportsPostReplaceUncertainty (store_test.go:133-154)
  - Verifies new token is visible despite sync failure (line 151-153)
  - Verifies CodeDurabilityUncertain error code (line 144)

## 7. Store.Rotate preserves uncertainty
- Rotate calls s.writeFile (store.go:91)
- Preserves CodeDurabilityUncertain if returned (store.go:92-93)
- Returns it without wrapping (line 93)

## 8. Temp cleanup/close
- createTokenExclusive: defer cleanup (store.go:184-189)
  - Sets closed flag when successful (line 206)
  - Defers close and remove (lines 185-188)
- writeTokenAtomicWithHooks: defer cleanup (rotate.go:27-34)
  - Sets closed flag when successful (line 51)
  - Sets committed flag when replaced (line 55)
  - Defers conditional cleanup (lines 28-33)

## 9. Private Unix dirs/files
- ensurePrivateDirectory: os.MkdirAll 0o700, secureTokenDirectory (store.go:160, 170)
- createTokenExclusive: temporary.Chmod(0o600) (store.go:190)
- writeTokenAtomicWithHooks: temporary.Chmod(mode.Perm()) (rotate.go:35)
- Test: TestStoreUnixPermissions (store_unix_test.go:13-36)
  - Verifies 0o700 directories and 0o600 token file

## 10. Symlink handling
- loadPath uses os.Lstat (store.go:109)
- Checks !info.Mode().IsRegular() && !privateTokenFile (store.go:116)
- ensurePrivateDirectory checks info.Mode()&os.ModeSymlink != 0 (store.go:167)
- Test: TestStoreRejectsSymlinks (store_unix_test.go:38-54)

## 11. Store mutex/random access
- Store.Load, LoadOrCreate, Rotate all use s.mu.Lock/Unlock (store.go:45, 52, 78)
- s.random is io.Reader field (store.go:23)
- No token leakage in error messages (test confirms canary not in error at store_test.go:114)

## 12. No token leakage
- Parse() error doesn't echo input (token.go:35, 39, 44)
- Error.Error() never includes token material (errors.go:29-42)
- Tests verify: TestTokenErrorDoesNotExposeMaterial (token_test.go:78-91)
- Tests verify: TestLoadRejectsCorruptTokenWithoutEcho (store_test.go:100-117)

## 13. Tests including separate Store concurrency
- ConcurrentLoadOrCreateConverges: 16 workers, same root (store_test.go:59-98)
- TestRotatePersistsAndFailureKeepsOldToken (store_test.go:156-186)
- Fuzz: FuzzParseToken (token_fuzz_test.go:4-16)
- Store oversize test: TestLoadRejectsOversizedTokenFile (store_test.go:119-131)

## 14. Injected uncertainty
- TestWriteTokenAtomicReportsPostReplaceUncertainty (store_test.go:133-154)
- TestRotatePersistsAndFailureKeepsOldToken with injected writeFile failure (store_test.go:176-186)

