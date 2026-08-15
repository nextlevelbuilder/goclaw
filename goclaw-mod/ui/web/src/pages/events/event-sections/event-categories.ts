import {
  ListTodo,
  MessageCircle,
  Bot,
  Settings,
  Link,
  type LucideIcon,
} from "lucide-react";

export interface EventCategoryConfig {
  label: string;
  icon: LucideIcon;
  borderColor: string;
  iconColor: string;
}

const teamTask: EventCategoryConfig = {
  label: "Task",
  icon: ListTodo,
  borderColor: "border-l-amber-500",
  iconColor: "text-amber-500",
};

const teamMessage: EventCategoryConfig = {
  label: "Message",
  icon: MessageCircle,
  borderColor: "border-l-green-500",
  iconColor: "text-green-500",
};

const agent: EventCategoryConfig = {
  label: "Agent",
  icon: Bot,
  borderColor: "border-l-orange-500",
  iconColor: "text-orange-500",
};

const teamCrud: EventCategoryConfig = {
  label: "Team",
  icon: Settings,
  borderColor: "border-l-gray-400",
  iconColor: "text-gray-400",
};

const agentLink: EventCategoryConfig = {
  label: "Link",
  icon: Link,
  borderColor: "border-l-cyan-500",
  iconColor: "text-cyan-500",
};

export function getCategoryConfig(event: string): EventCategoryConfig {
  if (event.startsWith("team.task.")) return teamTask;
  if (event === "team.message.sent") return teamMessage;
  if (event === "agent") return agent;
  if (event.startsWith("agent_link.")) return agentLink;
  if (
    event.startsWith("team.created") ||
    event.startsWith("team.updated") ||
    event.startsWith("team.deleted") ||
    event.startsWith("team.member.")
  ) {
    return teamCrud;
  }
  return teamCrud;
}
