import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useHttp } from "@/hooks/use-ws";
import { queryKeys } from "@/lib/query-keys";
import type { MergeContactsRequest, MergeContactsResponse } from "@/types/contact";

export function useContactMerge() {
  const http = useHttp();
  const queryClient = useQueryClient();

  const merge = useCallback(
    async (body: MergeContactsRequest) => {
      const res = await http.post<MergeContactsResponse>("/v1/contacts/merge", body);
      await queryClient.invalidateQueries({ queryKey: queryKeys.contacts.all });
      await queryClient.invalidateQueries({ queryKey: queryKeys.tenantUsers.all });
      return res;
    },
    [http, queryClient],
  );

  const unmerge = useCallback(
    async (contactIds: string[]) => {
      const res = await http.post<{ unmerged_count: number }>("/v1/contacts/unmerge", {
        contact_ids: contactIds,
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.contacts.all });
      return res;
    },
    [http, queryClient],
  );

  return { merge, unmerge };
}
