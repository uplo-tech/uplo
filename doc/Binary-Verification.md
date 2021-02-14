## Verifying Release Signatures

If a verification step fails, please contact https://support.uplo.tech/ for
additional information or to find a support contact before using the binary 

1. First you need to download and import the correct `gpg` key. This key will not be changed without advanced notice.
  - `wget -c https://github.com/uplo-tech/uplo/raw/master/doc/developer-pubkeys/uplo-signing-key.asc`
  - `gpg --import uplo-signing-key.asc`

2. Download the `SHA256SUMS` file for the release.
  - `wget -c http://github.com/uplo-tech/uplo/raw/master/doc/Uplo-1.x.x-SHA256SUMS.txt.asc`

3. Verify the signature.
   - `gpg --verify Uplo-1.x.x-SHA256SUMS.txt.asc`
   
   **If the output of that command fails STOP AND DO NOT USE THAT BINARY.**

4. Hash your downloaded binary.
  - `shasum256 ./uplod` or similar, providing the path to the binary as the first argument.
	 
   **If the output of that command is not found in `Uplo-1.x.x-SHA256SUMS.txt.asc` STOP AND DO NOT USE THAT BINARY.**
