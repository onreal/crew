package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	runtimeadapter "crew/internal/adapters/runtime"
	"crew/internal/application"
	"crew/internal/domain"
	"crew/internal/platform"
)

type buildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type runtimeState struct {
	build      buildInfo
	configPath string
	actors     string
	loaded     platform.LoadedConfig
}

var sessionStartAutoAttachDetector = supportsFullScreenTUI

func newRootCmd(build buildInfo) *cobra.Command {
	state := &runtimeState{build: build}

	root := &cobra.Command{
		Use:           "crew",
		Short:         "Operate a local-first multi-agent runtime from the terminal",
		Long:          "crew is a Go-based CLI for running and observing multi-agent sessions in free and sequential modes.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&state.configPath, "config", "", "Path to a YAML configuration file")
	root.PersistentFlags().StringVar(&state.actors, "actors", "", "Actor catalog selector under the active crew_agents directory; defaults to the root catalog")

	root.AddCommand(
		newInitCmd(),
		newVersionCmd(state),
		newConfigCmd(state),
		newAgentsCmd(state),
		newSessionCmd(state),
		newTaskCmd(state),
		newVectorCmd(state),
		newTUICmd(state),
	)
	root.AddCommand(newHelpCmd(root))

	return root
}

func (s *runtimeState) bootstrap() error {
	if s.loaded.Config.App.Name != "" {
		return nil
	}

	loaded, err := platform.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	if _, err := platform.NewLogger(loaded.Config.App.LogLevel); err != nil {
		return err
	}

	s.loaded = loaded

	return nil
}

func newVersionCmd(state *runtimeState) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build metadata",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, state.build)
		},
	}
}

func newConfigCmd(state *runtimeState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect configuration and runtime defaults",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the resolved configuration as JSON",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := state.bootstrap(); err != nil {
				return err
			}

			return writeJSON(cmd, map[string]any{
				"config_path": state.loaded.Path,
				"config":      state.loaded.Config,
			})
		},
	})

	var syncTargetPath string
	syncCmd := &cobra.Command{
		Use:   "sync [source-config-path]",
		Short: "Copy a YAML config into the installed default config path",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sourcePath := ""
			if len(args) > 0 {
				sourcePath = strings.TrimSpace(args[0])
			}
			if sourcePath == "" {
				if err := state.bootstrap(); err != nil {
					return err
				}
				sourcePath = strings.TrimSpace(state.loaded.Path)
				if sourcePath == "" {
					return newCLIError("invalid_configuration", "config sync requires a source YAML file; pass crew config sync ./crew.yaml or use --config")
				}
			} else {
				loaded, err := platform.LoadConfig(sourcePath)
				if err != nil {
					return err
				}
				sourcePath = loaded.Path
			}

			targetPath := strings.TrimSpace(syncTargetPath)
			if targetPath == "" {
				var err error
				targetPath, err = platform.DefaultConfigPath()
				if err != nil {
					return err
				}
			}

			sourceAbs, err := filepath.Abs(sourcePath)
			if err != nil {
				return newCLIError("command_failed", fmt.Sprintf("resolve source config %q: %v", sourcePath, err))
			}
			targetAbs, err := filepath.Abs(targetPath)
			if err != nil {
				return newCLIError("command_failed", fmt.Sprintf("resolve target config %q: %v", targetPath, err))
			}
			if sourceAbs == targetAbs {
				return writeJSON(cmd, map[string]any{
					"synced":             true,
					"source_config_path": sourceAbs,
					"target_config_path": targetAbs,
					"changed":            false,
				})
			}

			content, err := os.ReadFile(sourceAbs)
			if err != nil {
				return newCLIError("command_failed", fmt.Sprintf("read source config %q: %v", sourceAbs, err))
			}
			if err := os.MkdirAll(filepath.Dir(targetAbs), 0o755); err != nil {
				return newCLIError("command_failed", fmt.Sprintf("create config directory for %q: %v", targetAbs, err))
			}
			if err := os.WriteFile(targetAbs, content, 0o600); err != nil {
				return newCLIError("command_failed", fmt.Sprintf("write target config %q: %v", targetAbs, err))
			}

			return writeJSON(cmd, map[string]any{
				"synced":             true,
				"source_config_path": sourceAbs,
				"target_config_path": targetAbs,
				"changed":            true,
			})
		},
	}
	syncCmd.Flags().StringVar(&syncTargetPath, "target", "", "Optional target path; defaults to the installed wrapper config path")
	cmd.AddCommand(syncCmd)

	return cmd
}

func newAgentsCmd(state *runtimeState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "Inspect the filesystem-backed agent catalog",
	}

	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List agents loaded from the active catalog directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsDir, err := state.resolveActiveAgentsDir()
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}
			agents, err := runtimeadapter.LoadAgentsDir(agentsDir)
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}

			return writeJSON(cmd, map[string]any{
				"actors":     state.actors,
				"agents_dir": agentsDir,
				"agents":     agents,
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Validate all agent files in the active catalog directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsDir, err := agentsDirResolver()
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}
			agents, err := runtimeadapter.LoadAgentsDir(agentsDir)
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}

			ids := make([]string, 0, len(agents))
			for _, agent := range agents {
				ids = append(ids, string(agent.ID))
			}

			return writeJSON(cmd, map[string]any{
				"actors":      state.actors,
				"agents_dir":  agentsDir,
				"valid":       true,
				"agent_count": len(agents),
				"agent_ids":   ids,
			})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Persist the current agent catalog into the runtime backing store",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			agentsDir, err := state.resolveActiveAgentsDir()
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}
			agents, err := runtimeadapter.LoadAgentsDir(agentsDir)
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}

			result, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				ids := make([]string, 0, len(agents))
				for _, agent := range agents {
					ids = append(ids, string(agent.ID))
				}
				if err := rt.SyncAgentCatalog(agents); err != nil {
					return nil, err
				}
				return map[string]any{
					"actors":       state.actors,
					"agents_dir":   agentsDir,
					"synced":       true,
					"agent_count":  len(ids),
					"agent_ids":    ids,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	})

	return cmd
}

func newHelpCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help",
		Short: "List available commands",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return writeJSON(cmd, map[string]any{
				"commands": listCommands(root),
			})
		},
	}
}

func newSessionCmd(state *runtimeState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "session",
		Short: "Manage multi-agent sessions",
	}

	var mode string
	var startReasoning bool
	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Create and start a new local session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveMode := mode
			if effectiveMode == "" {
				if err := state.bootstrap(); err != nil {
					return err
				}
				effectiveMode = state.loaded.Config.Session.Mode
			}

			if err := validateMode(effectiveMode); err != nil {
				return err
			}

			var session domain.Session
			_, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				createdSession, err := rt.CreateSession(cmd.Context(), domain.SessionMode(effectiveMode))
				if err != nil {
					return nil, err
				}

				startedSession, err := rt.StartSession(cmd.Context(), createdSession.ID)
				if err != nil {
					return nil, err
				}
				session = startedSession

				return nil, nil
			})
			if err != nil {
				return err
			}

			if shouldAutoAttachOnSessionStart(domain.SessionMode(effectiveMode), cmd.InOrStdin(), cmd.OutOrStdout()) {
				options, err := state.defaultSessionStartAttachOptions(session.ID)
				if err != nil {
					return err
				}
				options.Reasoning = startReasoning
				return state.runTUISessionView(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), options)
			}

			return writeJSON(cmd, map[string]any{
				"session":      session,
				"storage_path": state.loaded.Config.Storage.Path,
			})
		},
	}
	startCmd.Flags().StringVar(&mode, "mode", "", "Session mode override: free or sequential")
	startCmd.Flags().BoolVar(&startReasoning, "reasoning", false, "When auto-attaching the room on a real terminal, show live provider progress inline in the chat")

	var sessionID string
	pauseCmd := &cobra.Command{
		Use:   "pause",
		Short: "Pause a running session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := state.runSessionMutation(cmd.Context(), sessionID, func(rt *runtimeadapter.Runtime, id domain.SessionID) (any, error) {
				session, err := rt.PauseSession(cmd.Context(), id)
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"session":      session,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	pauseCmd.Flags().StringVar(&sessionID, "session-id", "", "Session ID")

	var resumeSessionID string
	resumeCmd := &cobra.Command{
		Use:   "resume",
		Short: "Resume a paused session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := state.runSessionMutation(cmd.Context(), resumeSessionID, func(rt *runtimeadapter.Runtime, id domain.SessionID) (any, error) {
				session, err := rt.ResumeSession(cmd.Context(), id)
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"session":      session,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	resumeCmd.Flags().StringVar(&resumeSessionID, "session-id", "", "Session ID")

	var stopSessionID string
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop an active session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			result, err := state.runSessionMutation(cmd.Context(), stopSessionID, func(rt *runtimeadapter.Runtime, id domain.SessionID) (any, error) {
				session, err := rt.StopSession(cmd.Context(), id)
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"session":      session,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	stopCmd.Flags().StringVar(&stopSessionID, "session-id", "", "Session ID")

	var inspectSessionID string
	inspectCmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect persisted session state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseSessionID(inspectSessionID)
			if err != nil {
				return err
			}

			result, err := state.withLocalRuntime(cmd.Context(), false, func(rt *runtimeadapter.Runtime) (any, error) {
				snapshot, err := rt.InspectSession(cmd.Context(), id)
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"snapshot":     snapshot,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	inspectCmd.Flags().StringVar(&inspectSessionID, "session-id", "", "Session ID")

	var tailSessionID string
	var tailConversationID string
	var tailFollow bool
	var tailPollIntervalMillis int
	tailCmd := &cobra.Command{
		Use:   "tail",
		Short: "Print the persisted session stream as a live text view",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseSessionID(tailSessionID)
			if err != nil {
				return err
			}
			conversationID, err := parseOptionalConversationID(tailConversationID)
			if err != nil {
				return err
			}
			if tailPollIntervalMillis < 0 {
				return newCLIError("invalid_arguments", "--poll-interval-millis must be >= 0")
			}

			return state.runLiveSessionView(cmd.Context(), cmd.OutOrStdout(), liveViewOptions{
				SessionID:      sessionID,
				ConversationID: conversationID,
				Follow:         tailFollow,
				PollInterval:   time.Duration(tailPollIntervalMillis) * time.Millisecond,
			})
		},
	}
	tailCmd.Flags().StringVar(&tailSessionID, "session-id", "", "Session ID")
	tailCmd.Flags().StringVar(&tailConversationID, "conversation-id", "", "Optional conversation ID filter")
	tailCmd.Flags().BoolVar(&tailFollow, "follow", false, "Keep polling for new persisted stream entries")
	tailCmd.Flags().IntVar(&tailPollIntervalMillis, "poll-interval-millis", 0, "Polling interval override in milliseconds; defaults to ui.refresh_interval_millis")

	var stepSessionID string
	var stepConversationID string
	var stepOrchestration string
	var stepReplyRouting string
	stepCmd := &cobra.Command{
		Use:   "step",
		Short: "Execute one deterministic free-mode agent turn",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseSessionID(stepSessionID)
			if err != nil {
				return err
			}
			conversationID, err := parseOptionalConversationID(stepConversationID)
			if err != nil {
				return err
			}
			orchestrationMode, err := parseOptionalOrchestrationMode(stepOrchestration)
			if err != nil {
				return err
			}
			replyRoutingMode, err := parseOptionalReplyRoutingMode(stepReplyRouting)
			if err != nil {
				return err
			}

			result, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				step, err := rt.StepSession(cmd.Context(), application.StepSessionCommand{
					SessionID:         sessionID,
					ConversationID:    conversationID,
					OrchestrationMode: orchestrationMode,
					ReplyRoutingMode:  replyRoutingMode,
				})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"step":         step,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	stepCmd.Flags().StringVar(&stepSessionID, "session-id", "", "Session ID")
	stepCmd.Flags().StringVar(&stepConversationID, "conversation-id", "", "Conversation ID; defaults to the most recent conversation in the session")
	stepCmd.Flags().StringVar(&stepOrchestration, "orchestration", "", "Optional orchestration mode override: deterministic, round_robin, or mentioned_first")
	stepCmd.Flags().StringVar(&stepReplyRouting, "reply-routing", "", "Optional reply routing override: latest_speaker or reply_obligations")

	var autoSessionID string
	var autoConversationID string
	var autoMaxSteps int
	var autoOrchestration string
	var autoReplyRouting string
	autoCmd := &cobra.Command{
		Use:   "auto",
		Short: "Execute a bounded multi-turn free-mode run",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseSessionID(autoSessionID)
			if err != nil {
				return err
			}
			conversationID, err := parseOptionalConversationID(autoConversationID)
			if err != nil {
				return err
			}
			orchestrationMode, err := parseOptionalOrchestrationMode(autoOrchestration)
			if err != nil {
				return err
			}
			replyRoutingMode, err := parseOptionalReplyRoutingMode(autoReplyRouting)
			if err != nil {
				return err
			}
			if autoMaxSteps < 1 {
				return newCLIError("invalid_arguments", "--max-steps must be >= 1")
			}

			result, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				auto, err := rt.AutoSession(cmd.Context(), application.AutoSessionCommand{
					SessionID:         sessionID,
					ConversationID:    conversationID,
					MaxSteps:          autoMaxSteps,
					OrchestrationMode: orchestrationMode,
					ReplyRoutingMode:  replyRoutingMode,
				})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"auto":         auto,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	autoCmd.Flags().StringVar(&autoSessionID, "session-id", "", "Session ID")
	autoCmd.Flags().StringVar(&autoConversationID, "conversation-id", "", "Conversation ID; defaults to the most recent conversation in the session")
	autoCmd.Flags().IntVar(&autoMaxSteps, "max-steps", 0, "Maximum number of free-mode turns to execute")
	autoCmd.Flags().StringVar(&autoOrchestration, "orchestration", "", "Optional orchestration mode override: deterministic, round_robin, or mentioned_first")
	autoCmd.Flags().StringVar(&autoReplyRouting, "reply-routing", "", "Optional reply routing override: latest_speaker or reply_obligations")

	var sendSessionID string
	var sendConversationID string
	var sendBody string
	var sendToAgents []string
	var sendReplyTo string
	sendCmd := &cobra.Command{
		Use:   "send",
		Short: "Persist a user message into a running session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseSessionID(sendSessionID)
			if err != nil {
				return err
			}
			if sendBody == "" {
				return newCLIError("invalid_arguments", "missing required --body")
			}

			conversationID, err := parseConversationIDOrDefault(sendConversationID)
			if err != nil {
				return err
			}
			toAgentIDs, err := parseAgentIDs(sendToAgents)
			if err != nil {
				return err
			}
			replyTo, err := parseOptionalMessageID(sendReplyTo)
			if err != nil {
				return err
			}
			channel := domain.MessageChannelUser
			var policy *domain.ConversationPolicy
			if len(toAgentIDs) > 0 {
				channel = domain.MessageChannelDirect
				overridden := domain.DefaultConversationPolicy()
				overridden.RequireReplyTargetForDirect = false
				policy = &overridden
			}

			result, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				message, err := rt.DispatchMessage(cmd.Context(), application.DispatchMessageCommand{
					SessionID:      sessionID,
					ConversationID: conversationID,
					Sender:         domain.UserSender("operator"),
					ToAgentIDs:     toAgentIDs,
					Channel:        channel,
					Kind:           domain.MessageKindUtterance,
					Body:           sendBody,
					ReplyTo:        replyTo,
					Policy:         policy,
				})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"message":      message,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	sendCmd.Flags().StringVar(&sendSessionID, "session-id", "", "Session ID")
	sendCmd.Flags().StringVar(&sendConversationID, "conversation-id", "", "Conversation ID; defaults to conversation-1")
	sendCmd.Flags().StringVar(&sendBody, "body", "", "Message body")
	sendCmd.Flags().StringSliceVar(&sendToAgents, "to-agent", nil, "Optional direct recipient agent ID; repeat to target multiple agents")
	sendCmd.Flags().StringVar(&sendReplyTo, "reply-to", "", "Optional message ID this message replies to")

	var recallSessionID string
	var recallQuery string
	var recallLimit int
	recallCmd := &cobra.Command{
		Use:   "recall",
		Short: "Recall relevant session messages with vector fallback behavior",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseSessionID(recallSessionID)
			if err != nil {
				return err
			}
			if recallQuery == "" {
				return newCLIError("invalid_arguments", "missing required --query")
			}

			limit := recallLimit
			if limit == 0 {
				if err := state.bootstrap(); err != nil {
					return err
				}
				limit = state.loaded.Config.Vector.DefaultRecallLimit
			}
			if limit < 1 {
				return newCLIError("invalid_arguments", "limit must be >= 1")
			}

			result, err := state.withLocalRuntime(cmd.Context(), false, func(rt *runtimeadapter.Runtime) (any, error) {
				recall, err := rt.RecallSessionMessages(cmd.Context(), application.RecallSessionMessagesQuery{
					SessionID: id,
					QueryText: recallQuery,
					Limit:     limit,
				})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"recall":       recall,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	recallCmd.Flags().StringVar(&recallSessionID, "session-id", "", "Session ID")
	recallCmd.Flags().StringVar(&recallQuery, "query", "", "Recall query text")
	recallCmd.Flags().IntVar(&recallLimit, "limit", 0, "Maximum number of recalled messages")

	cmd.AddCommand(startCmd)
	cmd.AddCommand(pauseCmd)
	cmd.AddCommand(resumeCmd)
	cmd.AddCommand(stopCmd)
	cmd.AddCommand(inspectCmd)
	cmd.AddCommand(tailCmd)
	cmd.AddCommand(stepCmd)
	cmd.AddCommand(autoCmd)
	cmd.AddCommand(sendCmd)
	cmd.AddCommand(recallCmd)

	return cmd
}

func newVectorCmd(state *runtimeState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vector",
		Short: "Inspect and control optional vector indexing state",
	}

	var statusSessionID string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show vector backend and index state",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseOptionalSessionID(statusSessionID)
			if err != nil {
				return err
			}

			result, err := state.withLocalRuntime(cmd.Context(), false, func(rt *runtimeadapter.Runtime) (any, error) {
				indexState, backendStatus, err := rt.VectorStatus(cmd.Context(), application.VectorStatusQuery{SessionID: sessionID})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"backend_status": backendStatus,
					"index_state":    indexState,
					"storage_path":   state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	statusCmd.Flags().StringVar(&statusSessionID, "session-id", "", "Optional Session ID")

	var rebuildSessionID string
	var force bool
	rebuildCmd := &cobra.Command{
		Use:   "rebuild",
		Short: "Rebuild derived vector ownership rows from canonical messages",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseOptionalSessionID(rebuildSessionID)
			if err != nil {
				return err
			}

			result, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				stats, indexState, backendStatus, err := rt.RebuildVectors(cmd.Context(), application.VectorRebuildCommand{
					SessionID: sessionID,
					Force:     force,
				})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"stats":          stats,
					"backend_status": backendStatus,
					"index_state":    indexState,
					"storage_path":   state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	rebuildCmd.Flags().StringVar(&rebuildSessionID, "session-id", "", "Optional Session ID")
	rebuildCmd.Flags().BoolVar(&force, "force", false, "Force rebuild even when the stored fingerprint matches")

	cmd.AddCommand(statusCmd, rebuildCmd)

	return cmd
}

func newTaskCmd(state *runtimeState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage persisted sandbox tasks",
	}

	var createSessionID string
	var createConversationID string
	var createInstruction string
	var createTaskID string
	var createWorkspaceRoot string
	var createRequestedBy string
	var createAssignedTo string
	var createPermissionProfile string
	var createRuntimeName string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a persisted sandbox task for a configured runtime",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseSessionID(createSessionID)
			if err != nil {
				return err
			}
			if createInstruction == "" {
				return newCLIError("invalid_arguments", "missing required --instruction")
			}
			conversationID, err := parseConversationIDOrDefault(createConversationID)
			if err != nil {
				return err
			}
			if err := state.bootstrap(); err != nil {
				return err
			}
			runtimeName := strings.TrimSpace(createRuntimeName)
			if runtimeName == "" {
				runtimeName = strings.TrimSpace(state.loaded.Config.Sandbox.DefaultProvider)
			}
			if runtimeName == "" || runtimeName == "disabled" {
				return newCLIError("invalid_configuration", "sandbox.default_provider must be configured to create sandbox tasks")
			}
			if _, ok := state.loaded.Config.Sandbox.Providers[runtimeName]; !ok {
				return newCLIError("invalid_configuration", fmt.Sprintf("sandbox provider %q is not configured under sandbox.providers", runtimeName))
			}

			taskID := createTaskID
			if taskID == "" {
				taskID, err = newAgentTaskID()
				if err != nil {
					return err
				}
			}
			workspaceRoot, err := resolveWorkspaceRoot(createWorkspaceRoot)
			if err != nil {
				return err
			}

			permissionProfile := createPermissionProfile
			if permissionProfile == "" {
				permissionProfile = state.loaded.Config.Sandbox.PermissionProfile
			}

			result, err := state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				task, err := rt.CreateSandboxTask(cmd.Context(), application.CreateSandboxTaskCommand{
					TaskID:             application.AgentTaskID(taskID),
					SessionID:          sessionID,
					ConversationID:     conversationID,
					RequestedByAgentID: domain.AgentID(createRequestedBy),
					AssignedAgentID:    domain.AgentID(createAssignedTo),
					AssignedProvider:   application.AgentProviderClassSandboxedRuntime,
					RuntimeName:        runtimeName,
					WorkspaceRoot:      workspaceRoot,
					PermissionProfile:  application.SandboxPermissionProfile(permissionProfile),
					Instruction:        createInstruction,
				})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"task":         task,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}

			return writeJSON(cmd, result)
		},
	}
	createCmd.Flags().StringVar(&createSessionID, "session-id", "", "Session ID")
	createCmd.Flags().StringVar(&createConversationID, "conversation-id", "", "Conversation ID; defaults to conversation-1")
	createCmd.Flags().StringVar(&createInstruction, "instruction", "", "Sandbox task instruction")
	createCmd.Flags().StringVar(&createTaskID, "task-id", "", "Optional task ID override")
	createCmd.Flags().StringVar(&createWorkspaceRoot, "workspace-root", ".", "Source workspace root to copy into the sandbox")
	createCmd.Flags().StringVar(&createRequestedBy, "requested-by-agent-id", "", "Optional requesting agent ID")
	createCmd.Flags().StringVar(&createAssignedTo, "assigned-agent-id", "", "Optional assigned agent ID")
	createCmd.Flags().StringVar(&createRuntimeName, "runtime", "", "Configured sandbox runtime name; defaults to sandbox.default_provider")
	createCmd.Flags().StringVar(&createPermissionProfile, "permission-profile", "", "Permission profile override: read_only, patch, or full_task")

	var runTaskID string
	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Execute a pending sandbox task with the configured runtime router",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := parseAgentTaskID(runTaskID)
			if err != nil {
				return err
			}

			var result any
			result, err = state.withLocalRuntime(cmd.Context(), true, func(rt *runtimeadapter.Runtime) (any, error) {
				task, execErr := rt.ExecuteSandboxTask(cmd.Context(), application.ExecuteSandboxTaskCommand{
					TaskID: taskID,
				})
				payload := map[string]any{
					"task":         task,
					"storage_path": state.loaded.Config.Storage.Path,
				}
				if execErr != nil {
					return payload, execErr
				}
				return payload, nil
			})
			if result != nil {
				if writeErr := writeJSON(cmd, result); writeErr != nil {
					return writeErr
				}
			}
			if err != nil {
				return err
			}
			return nil
		},
	}
	runCmd.Flags().StringVar(&runTaskID, "task-id", "", "Task ID")

	var listSessionID string
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List sandbox tasks for a session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID, err := parseSessionID(listSessionID)
			if err != nil {
				return err
			}

			result, err := state.withLocalRuntime(cmd.Context(), false, func(rt *runtimeadapter.Runtime) (any, error) {
				tasks, err := rt.ListSandboxTasksBySession(cmd.Context(), application.ListSandboxTasksQuery{SessionID: sessionID})
				if err != nil {
					return nil, err
				}

				return map[string]any{
					"tasks":        tasks,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd, result)
		},
	}
	listCmd.Flags().StringVar(&listSessionID, "session-id", "", "Session ID")

	var getTaskID string
	getCmd := &cobra.Command{
		Use:   "get",
		Short: "Inspect one sandbox task",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID, err := parseAgentTaskID(getTaskID)
			if err != nil {
				return err
			}

			result, err := state.withLocalRuntime(cmd.Context(), false, func(rt *runtimeadapter.Runtime) (any, error) {
				task, err := rt.GetSandboxTask(cmd.Context(), application.GetSandboxTaskQuery{TaskID: taskID})
				if err != nil {
					return nil, err
				}
				return map[string]any{
					"task":         task,
					"storage_path": state.loaded.Config.Storage.Path,
				}, nil
			})
			if err != nil {
				return err
			}
			return writeJSON(cmd, result)
		},
	}
	getCmd.Flags().StringVar(&getTaskID, "task-id", "", "Task ID")

	cmd.AddCommand(createCmd, runCmd, listCmd, getCmd)
	return cmd
}

func newTUICmd(state *runtimeState) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Operate the terminal user interface",
	}

	var attachSessionID string
	var attachConversationID string
	var attachFollow bool
	var attachPollIntervalMillis int
	var attachAutoSteps int
	var attachOrchestration string
	var attachReplyRouting string
	var attachDebug bool
	var attachReasoning bool
	var attachTerminalScrollback bool
	attachCmd := &cobra.Command{
		Use:   "attach",
		Short: "Attach an interactive live text session view in the terminal",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := state.bootstrap(); err != nil {
				return err
			}
			sessionID, err := parseSessionID(attachSessionID)
			if err != nil {
				return err
			}
			conversationID, err := parseOptionalConversationID(attachConversationID)
			if err != nil {
				return err
			}
			if attachPollIntervalMillis < 0 {
				return newCLIError("invalid_arguments", "--poll-interval-millis must be >= 0")
			}
			if attachAutoSteps < -1 {
				return newCLIError("invalid_arguments", "--auto-steps must be >= 0")
			}
			autoSteps := attachAutoSteps
			if autoSteps == -1 {
				autoSteps = state.loaded.Config.UI.AttachAutoSteps
			}
			orchestrationMode, err := parseOptionalOrchestrationMode(attachOrchestration)
			if err != nil {
				return err
			}
			if orchestrationMode == "" {
				orchestrationMode = application.OrchestrationMode(state.loaded.Config.Session.OrchestrationMode)
			}
			replyRoutingMode, err := parseOptionalReplyRoutingMode(attachReplyRouting)
			if err != nil {
				return err
			}
			if replyRoutingMode == "" {
				replyRoutingMode = application.ReplyRoutingMode(state.loaded.Config.Session.ReplyRoutingMode)
			}
			agentsDir, err := agentsDirResolver()
			if err != nil {
				return newCLIError("invalid_configuration", err.Error())
			}

			if err := state.runTUISessionView(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), liveViewOptions{
				SessionID:          sessionID,
				ConversationID:     conversationID,
				AgentsDir:          agentsDir,
				Follow:             attachFollow,
				TerminalScrollback: attachTerminalScrollback,
				PollInterval:       time.Duration(attachPollIntervalMillis) * time.Millisecond,
				PrintHeader:        true,
				AutoSteps:          autoSteps,
				Orchestration:      orchestrationMode,
				ReplyRouting:       replyRoutingMode,
				Debug:              attachDebug,
				Reasoning:          attachReasoning,
			}); err != nil {
				return err
			}
			return nil
		},
	}
	attachCmd.Flags().StringVar(&attachSessionID, "session-id", "", "Session ID")
	attachCmd.Flags().StringVar(&attachConversationID, "conversation-id", "", "Initial send conversation ID; the room still shows the full session timeline")
	attachCmd.Flags().BoolVar(&attachFollow, "follow", true, "Keep polling for new persisted stream entries")
	attachCmd.Flags().IntVar(&attachPollIntervalMillis, "poll-interval-millis", 0, "Polling interval override in milliseconds; defaults to ui.refresh_interval_millis")
	attachCmd.Flags().IntVar(&attachAutoSteps, "auto-steps", -1, "After plain text input, run up to N free-mode turns in the attached conversation; defaults to ui.attach_auto_steps")
	attachCmd.Flags().StringVar(&attachOrchestration, "orchestration", "", "Optional orchestration mode override: deterministic, round_robin, or mentioned_first")
	attachCmd.Flags().StringVar(&attachReplyRouting, "reply-routing", "", "Optional reply routing override: latest_speaker or reply_obligations")
	attachCmd.Flags().BoolVar(&attachDebug, "debug", false, "Show transcript metadata such as timestamps, conversation IDs, and reply target IDs")
	attachCmd.Flags().BoolVar(&attachReasoning, "reasoning", false, "Show live provider progress inline in the chat while a turn is running")
	attachCmd.Flags().BoolVar(&attachTerminalScrollback, "terminal-scrollback", false, "Use append-only terminal output instead of the managed fullscreen room so history stays in terminal scrollback and mouse wheel works natively")

	cmd.AddCommand(attachCmd)

	return cmd
}

func shouldAutoAttachOnSessionStart(mode domain.SessionMode, in io.Reader, out io.Writer) bool {
	return mode == domain.SessionModeFree && sessionStartAutoAttachDetector(in, out)
}

func (s *runtimeState) defaultSessionStartAttachOptions(sessionID domain.SessionID) (liveViewOptions, error) {
	if err := s.bootstrap(); err != nil {
		return liveViewOptions{}, err
	}

	agentsDir, err := agentsDirResolver()
	if err != nil {
		return liveViewOptions{}, newCLIError("invalid_configuration", err.Error())
	}

	return liveViewOptions{
		SessionID:     sessionID,
		AgentsDir:     agentsDir,
		Follow:        true,
		PrintHeader:   true,
		AutoSteps:     s.loaded.Config.UI.AttachAutoSteps,
		Orchestration: application.OrchestrationMode(s.loaded.Config.Session.OrchestrationMode),
		ReplyRouting:  application.ReplyRoutingMode(s.loaded.Config.Session.ReplyRoutingMode),
	}, nil
}

func writeJSON(cmd *cobra.Command, value any) error {
	encoder := json.NewEncoder(cmd.OutOrStdout())
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func validateMode(mode string) error {
	switch mode {
	case "free", "sequential":
		return nil
	default:
		return newCLIError("invalid_arguments", fmt.Sprintf("invalid session mode %q: must be free or sequential", mode))
	}
}

func signalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return platform.NewSignalContext(parent)
}

func (s *runtimeState) runSessionMutation(
	ctx context.Context,
	rawSessionID string,
	fn func(rt *runtimeadapter.Runtime, sessionID domain.SessionID) (any, error),
) (any, error) {
	sessionID, err := parseSessionID(rawSessionID)
	if err != nil {
		return nil, err
	}

	return s.withLocalRuntime(ctx, true, func(rt *runtimeadapter.Runtime) (any, error) {
		return fn(rt, sessionID)
	})
}

func parseSessionID(raw string) (domain.SessionID, error) {
	if raw == "" {
		return "", newCLIError("invalid_arguments", "missing required --session-id")
	}

	sessionID := domain.SessionID(raw)
	if err := sessionID.Validate(); err != nil {
		return "", newCLIError("invalid_arguments", err.Error())
	}

	return sessionID, nil
}

func parseOptionalSessionID(raw string) (domain.SessionID, error) {
	if raw == "" {
		return "", nil
	}
	return parseSessionID(raw)
}

func parseConversationIDOrDefault(raw string) (domain.ConversationID, error) {
	if raw == "" {
		return domain.ConversationID("conversation-1"), nil
	}

	return parseOptionalConversationID(raw)
}

func parseOptionalConversationID(raw string) (domain.ConversationID, error) {
	if raw == "" {
		return "", nil
	}

	conversationID := domain.ConversationID(raw)
	if err := conversationID.Validate(); err != nil {
		return "", newCLIError("invalid_arguments", err.Error())
	}

	return conversationID, nil
}

func parseAgentTaskID(raw string) (application.AgentTaskID, error) {
	if raw == "" {
		return "", newCLIError("invalid_arguments", "missing required --task-id")
	}
	taskID := application.AgentTaskID(raw)
	if err := taskID.Validate(); err != nil {
		return "", newCLIError("invalid_arguments", err.Error())
	}
	return taskID, nil
}

func parseAgentIDs(raw []string) ([]domain.AgentID, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	seen := make(map[domain.AgentID]struct{}, len(raw))
	ids := make([]domain.AgentID, 0, len(raw))
	for _, value := range raw {
		id := domain.AgentID(value)
		if err := id.Validate(); err != nil {
			return nil, newCLIError("invalid_arguments", err.Error())
		}
		if _, exists := seen[id]; exists {
			return nil, newCLIError("invalid_arguments", fmt.Sprintf("duplicate --to-agent value %q", id))
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	return ids, nil
}

func parseOptionalMessageID(raw string) (domain.MessageID, error) {
	if raw == "" {
		return "", nil
	}

	messageID := domain.MessageID(raw)
	if err := messageID.Validate(); err != nil {
		return "", newCLIError("invalid_arguments", err.Error())
	}

	return messageID, nil
}

func parseOptionalOrchestrationMode(raw string) (application.OrchestrationMode, error) {
	if raw == "" {
		return "", nil
	}

	mode := application.OrchestrationMode(raw)
	if err := mode.Validate(); err != nil {
		return "", newCLIError("invalid_arguments", err.Error())
	}

	return mode, nil
}

func parseOptionalReplyRoutingMode(raw string) (application.ReplyRoutingMode, error) {
	if raw == "" {
		return "", nil
	}

	mode := application.ReplyRoutingMode(raw)
	if err := mode.Validate(); err != nil {
		return "", newCLIError("invalid_arguments", err.Error())
	}

	return mode, nil
}

func newAgentTaskID() (string, error) {
	var bytes [8]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", newCLIError("command_failed", fmt.Sprintf("generate task id: %v", err))
	}
	return "task-" + hex.EncodeToString(bytes[:]), nil
}

func resolveWorkspaceRoot(raw string) (string, error) {
	value := raw
	if value == "" {
		value = "."
	}
	absPath, err := filepath.Abs(value)
	if err != nil {
		return "", newCLIError("invalid_arguments", fmt.Sprintf("resolve workspace root %q: %v", value, err))
	}
	info, err := os.Stat(absPath)
	if err != nil {
		return "", newCLIError("invalid_arguments", fmt.Sprintf("stat workspace root %q: %v", absPath, err))
	}
	if !info.IsDir() {
		return "", newCLIError("invalid_arguments", fmt.Sprintf("workspace root %q is not a directory", absPath))
	}
	return absPath, nil
}

func listCommands(root *cobra.Command) []map[string]any {
	commands := make([]map[string]any, 0)
	for _, child := range root.Commands() {
		commands = append(commands, flattenCommand(child, root.Name())...)
	}
	return commands
}

func flattenCommand(cmd *cobra.Command, prefix string) []map[string]any {
	if cmd.Hidden {
		return nil
	}

	path := prefix + " " + cmd.Name()
	commands := []map[string]any{{
		"command": path,
		"summary": cmd.Short,
	}}

	for _, child := range cmd.Commands() {
		commands = append(commands, flattenCommand(child, path)...)
	}

	return commands
}
