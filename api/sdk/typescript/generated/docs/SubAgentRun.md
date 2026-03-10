# SubAgentRun


## Properties

Name | Type | Description | Notes
------------ | ------------- | ------------- | -------------
**id** | **string** | Unique run ID | [optional] [default to undefined]
**parent_session** | **string** | Session ID of the orchestrator that spawned this sub-agent | [optional] [default to undefined]
**role** | **string** | Sub-agent role (researcher, coder, reviewer, generic) | [optional] [default to undefined]
**goal** | **string** | The task/prompt given to the sub-agent | [optional] [default to undefined]
**model** | **string** | Model override used for this run | [optional] [default to undefined]
**status** | **string** | Current run status | [optional] [default to undefined]
**output** | **string** | Final output produced by the sub-agent | [optional] [default to undefined]
**error** | **string** | Error message if the run failed | [optional] [default to undefined]
**prompt_tokens** | **number** |  | [optional] [default to undefined]
**output_tokens** | **number** |  | [optional] [default to undefined]
**total_cost** | **number** |  | [optional] [default to undefined]
**created_at** | **string** |  | [optional] [default to undefined]
**started_at** | **string** |  | [optional] [default to undefined]
**finished_at** | **string** |  | [optional] [default to undefined]

## Example

```typescript
import { SubAgentRun } from './api';

const instance: SubAgentRun = {
    id,
    parent_session,
    role,
    goal,
    model,
    status,
    output,
    error,
    prompt_tokens,
    output_tokens,
    total_cost,
    created_at,
    started_at,
    finished_at,
};
```

[[Back to Model list]](../README.md#documentation-for-models) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to README]](../README.md)
