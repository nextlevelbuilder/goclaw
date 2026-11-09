package protocol

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// GroupInfo holds the group fields needed for @mention resolution.
type GroupInfo struct {
	GroupID   string
	Name      string
	MemberIDs []string
}

// GroupMember is the profile subset returned by the group members endpoint.
type GroupMember struct {
	UID         string
	DisplayName string
	ZaloName    string
}

// GetGroupInfo fetches group metadata and its member version list. Zalo's
// getmg-v2 returns the member IDs in memVerList (or memberIds on older
// responses); both shapes are accepted.
func GetGroupInfo(ctx context.Context, sess *Session, groupID string) (*GroupInfo, error) {
	baseURL := getServiceURL(sess, "group")
	if baseURL == "" {
		return nil, fmt.Errorf("zalo_personal: no group service URL")
	}
	gridVer, err := json.Marshal(map[string]int{groupID: 0})
	if err != nil {
		return nil, err
	}
	encData, err := encryptPayload(sess, map[string]any{"gridVerMap": string(gridVer)})
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: encrypt group info payload: %w", err)
	}
	reqURL := makeURL(sess, baseURL+"/api/group/getmg-v2", nil, true)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, buildFormBody(map[string]string{"params": encData}))
	if err != nil {
		return nil, err
	}
	setDefaultHeaders(req, sess)
	resp, err := sess.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: get group info: %w", err)
	}
	defer resp.Body.Close()

	var envelope Response[*string]
	if err := readJSON(resp, &envelope); err != nil {
		return nil, fmt.Errorf("zalo_personal: parse group info response: %w", err)
	}
	if envelope.ErrorCode != 0 {
		return nil, NewAPIError("group_info", envelope.ErrorCode)
	}
	if envelope.Data == nil {
		return nil, fmt.Errorf("zalo_personal: empty group info response")
	}
	plain, err := decryptDataField(sess, *envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: decrypt group info: %w", err)
	}
	var result struct {
		GridInfoMap map[string]struct {
			Name       string   `json:"name"`
			MemVerList []string `json:"memVerList"`
			MemberIDs  []string `json:"memberIds"`
		} `json:"gridInfoMap"`
	}
	if err := json.Unmarshal(plain, &result); err != nil {
		return nil, fmt.Errorf("zalo_personal: parse group info: %w", err)
	}
	info, ok := result.GridInfoMap[groupID]
	if !ok {
		return nil, fmt.Errorf("zalo_personal: group %s not found in response", groupID)
	}
	members := info.MemVerList
	if len(members) == 0 {
		members = info.MemberIDs
	}
	for i, id := range members {
		members[i] = normalizeMemberID(id)
	}
	return &GroupInfo{GroupID: groupID, Name: info.Name, MemberIDs: members}, nil
}

// GetGroupMembersInfo fetches display names for group members. The profile
// service expects an encrypted params query containing friend_pversion_map.
func GetGroupMembersInfo(ctx context.Context, sess *Session, memberIDs []string) (map[string]GroupMember, error) {
	if len(memberIDs) == 0 {
		return nil, nil
	}
	baseURL := getServiceURL(sess, "profile")
	if baseURL == "" {
		return nil, fmt.Errorf("zalo_personal: no profile service URL")
	}
	versions := make([]string, 0, len(memberIDs))
	for _, id := range memberIDs {
		versions = append(versions, normalizeMemberID(id)+"_0")
	}
	encData, err := encryptPayload(sess, map[string]any{"friend_pversion_map": versions})
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: encrypt group members payload: %w", err)
	}
	reqURL := makeURL(sess, baseURL+"/api/social/group/members", map[string]any{"params": encData}, true)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	setDefaultHeaders(req, sess)
	resp, err := sess.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: get group members: %w", err)
	}
	defer resp.Body.Close()

	var envelope Response[*string]
	if err := readJSON(resp, &envelope); err != nil {
		return nil, fmt.Errorf("zalo_personal: parse group members response: %w", err)
	}
	if envelope.ErrorCode != 0 {
		return nil, NewAPIError("group_members", envelope.ErrorCode)
	}
	if envelope.Data == nil {
		return nil, nil
	}
	plain, err := decryptDataField(sess, *envelope.Data)
	if err != nil {
		return nil, fmt.Errorf("zalo_personal: decrypt group members: %w", err)
	}
	var result struct {
		Profiles map[string]struct {
			DisplayName string `json:"displayName"`
			ZaloName    string `json:"zaloName"`
		} `json:"profiles"`
	}
	if err := json.Unmarshal(plain, &result); err != nil {
		return nil, fmt.Errorf("zalo_personal: parse group members: %w", err)
	}
	members := make(map[string]GroupMember, len(result.Profiles))
	for uid, profile := range result.Profiles {
		members[uid] = GroupMember{UID: uid, DisplayName: profile.DisplayName, ZaloName: profile.ZaloName}
	}
	return members, nil
}

// normalizeMemberID strips a trailing "_<version>" suffix that Zalo's
// memVerList uses (e.g. "123456789_0" -> "123456789"). Plain memberIds pass
// through unchanged.
func normalizeMemberID(id string) string {
	id = strings.TrimSpace(id)
	idx := strings.LastIndexByte(id, '_')
	if idx <= 0 || idx == len(id)-1 {
		return id
	}
	for _, r := range id[idx+1:] {
		if r < '0' || r > '9' {
			return id
		}
	}
	return id[:idx]
}
