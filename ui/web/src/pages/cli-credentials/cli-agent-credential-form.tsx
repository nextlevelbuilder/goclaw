import { useTranslation } from "react-i18next";
import type { Dispatch, SetStateAction } from "react";
import { Label } from "@/components/ui/label";
import {
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue,
} from "@/components/ui/select";
import { CliCredentialEnvVarsSection, type ManualEnvEntry } from "./cli-credential-env-vars-section";
import { CliCredentialGitFields, type GitCredentialType } from "./cli-credential-git-fields";
import type { AgentData } from "@/types/agent";
import type { SecureCLIBinary } from "./hooks/use-cli-credentials";

interface Props {
  binary: SecureCLIBinary;
  agents: AgentData[];
  agentId: string;
  setAgentId: (v: string) => void;
  editing: boolean;
  envEntries: ManualEnvEntry[];
  setEnvEntries: Dispatch<SetStateAction<ManualEnvEntry[]>>;
  gitType: GitCredentialType;
  setGitType: (v: GitCredentialType) => void;
  gitHostScope: string;
  setGitHostScope: (v: string) => void;
  gitToken: string;
  setGitToken: (v: string) => void;
  gitPrivateKey: string;
  setGitPrivateKey: (v: string) => void;
  gitErrorKey?: string;
  gitHasExistingSecret: boolean;
}

export function CliAgentCredentialForm(props: Props) {
  const { t } = useTranslation("cli-credentials");
  const isGit = props.binary.adapter_name === "git";

  return (
    <div className="flex flex-col gap-4">
      <div className="grid gap-1.5">
        <Label>{t("agentCredentials.agent")}</Label>
        <Select value={props.agentId} onValueChange={props.setAgentId} disabled={props.editing}>
          <SelectTrigger className="text-base md:text-sm">
            <SelectValue placeholder={t("grants.selectAgent")} />
          </SelectTrigger>
          <SelectContent>
            {props.agents.map((a) => (
              <SelectItem key={a.id} value={a.id}>
                {a.display_name || a.agent_key}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {isGit ? (
        <CliCredentialGitFields
          type={props.gitType}
          onTypeChange={props.setGitType}
          hostScope={props.gitHostScope}
          onHostScopeChange={props.setGitHostScope}
          token={props.gitToken}
          onTokenChange={props.setGitToken}
          privateKey={props.gitPrivateKey}
          onPrivateKeyChange={props.setGitPrivateKey}
          errorKey={props.gitErrorKey}
          hasExistingSecret={props.gitHasExistingSecret}
        />
      ) : null}

      {(!isGit || props.gitType === "env") ? (
        <div className="grid gap-1.5">
          <Label>{t("agentCredentials.env")}</Label>
          <CliCredentialEnvVarsSection
            isManualMode
            activePreset={null}
            envValues={{}}
            setEnvValues={() => undefined}
            manualEnvEntries={props.envEntries}
            setManualEnvEntries={props.setEnvEntries}
          />
        </div>
      ) : null}
    </div>
  );
}
