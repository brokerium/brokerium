// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package brokerium embeds the license and notice files of the repository root into the server,
// which shows them on the Licenses page of the dashboard. `make notices` generates
// THIRD_PARTY_NOTICES and the third-party section of NOTICE.
package brokerium

import _ "embed"

// LegalFile is a license or notice file, named as in /licenses of the container images.
type LegalFile struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Text  string `json:"text"`
}

var (
	//go:embed LICENSE
	license string
	//go:embed NOTICE
	notice string
	//go:embed THIRD_PARTY_NOTICES
	thirdPartyNotices string
)

// editionFiles are the license files that only the edition of the build carries.
var editionFiles []LegalFile

// LegalFiles returns the license and notice files of the build.
func LegalFiles() []LegalFile {
	files := []LegalFile{{Name: "LICENSE", Title: "License", Text: license}}
	files = append(files, editionFiles...)
	return append(files,
		LegalFile{Name: "NOTICE", Title: "Notice", Text: notice},
		LegalFile{Name: "THIRD_PARTY_NOTICES", Title: "Third-party notices", Text: thirdPartyNotices})
}
