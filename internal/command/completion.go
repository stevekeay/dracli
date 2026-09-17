package command

import (
	"errors"
	"fmt"
	"io"
)

func runCompletion(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: dracli completion <bash|zsh>")
	}
	if args[0] == "--help" || args[0] == "-help" || args[0] == "-h" {
		_, err := io.WriteString(stdout, "Usage: dracli completion <bash|zsh>\n")
		return err
	}

	var script string
	switch args[0] {
	case "bash":
		script = bashCompletion
	case "zsh":
		script = zshCompletion
	default:
		return fmt.Errorf("unsupported shell %q: use bash or zsh", args[0])
	}
	_, err := io.WriteString(stdout, script)
	return err
}

const bashCompletion = `# bash completion for dracli
_dracli()
{
    local cur prev command command_index word index value prefix candidate
    local commands global_options common_options options

    COMPREPLY=()
    cur="${COMP_WORDS[COMP_CWORD]}"
    prev=""
    if (( COMP_CWORD > 0 )); then
        prev="${COMP_WORDS[COMP_CWORD-1]}"
    fi

    commands="completion logs password query inventory status jobs clear-jobs factory-reset settings help"
    global_options="--verify-tls --help -help -h"
    common_options="--username --password --verify-tls --output --timeout --help -help -h"

    command=""
    command_index=0
    for (( index=1; index<COMP_CWORD; index++ )); do
        word="${COMP_WORDS[index]}"
        case "$word" in
            completion|logs|password|query|inventory|status|jobs|clear-jobs|factory-reset|settings|help)
                command="$word"
                command_index=$index
                break
                ;;
        esac
    done

    if [[ -z "$command" ]]; then
        if [[ "$cur" == -* ]]; then
            COMPREPLY=( $(compgen -W "$global_options" -- "$cur") )
        else
            COMPREPLY=( $(compgen -W "$commands" -- "$cur") )
        fi
        return
    fi

    case "$prev" in
        --output)
            COMPREPLY=( $(compgen -W "text json" -- "$cur") )
            return
            ;;
        --username|--password|--timeout|--manager|--system|--interval|--name|--set)
            return
            ;;
    esac

    case "$cur" in
        --output=*)
            prefix="${cur%%=*}="
            value="${cur#*=}"
            while IFS= read -r candidate; do
                COMPREPLY+=("${prefix}${candidate}")
            done < <(compgen -W "text json" -- "$value")
            return
            ;;
    esac

    if [[ "$command" == "completion" ]]; then
        if (( COMP_CWORD == command_index + 1 )); then
            if [[ "$cur" == -* ]]; then
                COMPREPLY=( $(compgen -W "--help -help -h" -- "$cur") )
            else
                COMPREPLY=( $(compgen -W "bash zsh" -- "$cur") )
            fi
        fi
        return
    fi

    if [[ "$command" == "help" ]]; then
        return
    fi

    if [[ "$command" == "logs" ]] && (( COMP_CWORD == command_index + 1 )) && [[ "$cur" != -* ]]; then
        COMPREPLY=( $(compgen -W "system lc" -- "$cur") )
        return
    fi

    if [[ "$command" == "settings" ]] && (( COMP_CWORD == command_index + 1 )) && [[ "$cur" != -* ]]; then
        COMPREPLY=( $(compgen -W "drac bios" -- "$cur") )
        return
    fi

    case "$command" in
        logs)
            options="$common_options --manager --all"
            ;;
        password)
            options="--password --help -help -h"
            ;;
        query|inventory)
            options="$common_options --manager --system"
            ;;
        status)
            options="$common_options --system --monitor --interval"
            ;;
        jobs|clear-jobs)
            options="$common_options --manager"
            ;;
        factory-reset)
            options="$common_options --manager --yes"
            ;;
        settings)
            options="$common_options --manager --system --all --name --set"
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        COMPREPLY=( $(compgen -W "$options" -- "$cur") )
    fi
}

complete -F _dracli dracli
`

const zshCompletion = `#compdef dracli
# zsh completion for dracli
_dracli()
{
    local cur prev command word
    integer command_index index
    local -a commands global_options common_options options

    cur="${words[CURRENT]}"
    prev=""
    if (( CURRENT > 1 )); then
        prev="${words[CURRENT-1]}"
    fi

    commands=(
        'completion:Generate a shell completion script'
        'logs:Fetch system and Lifecycle Controller log entries'
        'password:Print the resolved iDRAC password'
        'query:Show a quick system summary'
        'inventory:Show system RAID and NIC inventory'
        'status:Show power state and boot progress'
        'jobs:Show the iDRAC job queue'
        'clear-jobs:Delete every entry in the iDRAC job queue'
        'factory-reset:Reset iDRAC settings to factory defaults'
        'settings:Show or change iDRAC and BIOS settings'
        'help:Show the command guide'
    )
    global_options=(
        '--verify-tls:Validate the BMC TLS certificate and hostname'
        '--help:Show the command guide'
        '-help:Show the command guide'
        '-h:Show the command guide'
    )
    common_options=(
        '--username:BMC username'
        '--password:BMC password'
        '--verify-tls:Validate the BMC TLS certificate and hostname'
        '--output:Output format (text or json)'
        '--timeout:HTTP request timeout'
        '--help:Show command help'
        '-help:Show command help'
        '-h:Show command help'
    )

    command=""
    command_index=0
    for (( index=2; index<CURRENT; index++ )); do
        word="${words[index]}"
        case "$word" in
            completion|logs|password|query|inventory|status|jobs|clear-jobs|factory-reset|settings|help)
                command="$word"
                command_index=$index
                break
                ;;
        esac
    done

    if [[ -z "$command" ]]; then
        if [[ "$cur" == -* ]]; then
            _describe -t options 'global option' global_options
        else
            _describe -t commands 'dracli command' commands
        fi
        return
    fi

    case "$prev" in
        --output)
            _values 'output format' text json
            return
            ;;
        --username|--password|--timeout|--manager|--system|--interval|--name|--set)
            return
            ;;
    esac

    case "$cur" in
        --output=*)
            compset -P '*='
            _values 'output format' text json
            return
            ;;
    esac

    if [[ "$command" == "completion" ]]; then
        if (( CURRENT == command_index + 1 )); then
            if [[ "$cur" == -* ]]; then
                _describe -t options 'option' global_options
            else
                _values 'shell' bash zsh
            fi
        fi
        return
    fi

    if [[ "$command" == "help" ]]; then
        return
    fi

    if [[ "$command" == "logs" ]] && (( CURRENT == command_index + 1 )) && [[ "$cur" != -* ]]; then
        _values 'log type' system lc
        return
    fi

    if [[ "$command" == "settings" ]] && (( CURRENT == command_index + 1 )) && [[ "$cur" != -* ]]; then
        _values 'settings namespace' drac bios
        return
    fi

    case "$command" in
        logs)
            options=(
                "${common_options[@]}"
                '--manager:Redfish manager identifier'
                '--all:Fetch every available log page'
            )
            ;;
        password)
            options=(
                '--password:Plaintext iDRAC password override'
                '--help:Show command help'
                '-help:Show command help'
                '-h:Show command help'
            )
            ;;
        query|inventory)
            options=(
                "${common_options[@]}"
                '--manager:Redfish manager identifier'
                '--system:Redfish system identifier'
            )
            ;;
        status)
            options=(
                "${common_options[@]}"
                '--system:Redfish system identifier'
                '--monitor:Poll and report state changes'
                '--interval:Monitor polling interval'
            )
            ;;
        jobs|clear-jobs)
            options=(
                "${common_options[@]}"
                '--manager:Redfish manager identifier'
            )
            ;;
        factory-reset)
            options=(
                "${common_options[@]}"
                '--manager:Redfish manager identifier'
                '--yes:Confirm the factory reset'
            )
            ;;
        settings)
            options=(
                "${common_options[@]}"
                '--manager:Redfish manager identifier'
                '--system:Redfish system identifier'
                '--all:Show every available setting'
                '--name:Show this setting (repeatable)'
                '--set:Set NAME=VALUE (repeatable)'
            )
            ;;
    esac

    if [[ "$cur" == -* ]]; then
        _describe -t options 'option' options
    fi
}

compdef _dracli dracli
`
