export namespace engine {
	
	export class Binding {
	    label?: string;
	    task?: models.TaskRef;
	
	    static createFrom(source: any = {}) {
	        return new Binding(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.label = source["label"];
	        this.task = this.convertValues(source["task"], models.TaskRef);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class State {
	    phase: string;
	    next_phase: string;
	    paused: boolean;
	    session_id?: number[];
	    // Go type: time
	    started_at?: any;
	    // Go type: time
	    paused_at?: any;
	    paused_total_seconds: number;
	    planned_seconds: number;
	    remaining_seconds: number;
	    label?: string;
	    task?: models.TaskRef;
	    completed_today: number;
	    day_blocks: number[];
	    block_index: number;
	    pos_in_block: number;
	    block_size: number;
	    day_total: number;
	    day_complete: boolean;
	    sound_enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new State(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.phase = source["phase"];
	        this.next_phase = source["next_phase"];
	        this.paused = source["paused"];
	        this.session_id = source["session_id"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.paused_at = this.convertValues(source["paused_at"], null);
	        this.paused_total_seconds = source["paused_total_seconds"];
	        this.planned_seconds = source["planned_seconds"];
	        this.remaining_seconds = source["remaining_seconds"];
	        this.label = source["label"];
	        this.task = this.convertValues(source["task"], models.TaskRef);
	        this.completed_today = source["completed_today"];
	        this.day_blocks = source["day_blocks"];
	        this.block_index = source["block_index"];
	        this.pos_in_block = source["pos_in_block"];
	        this.block_size = source["block_size"];
	        this.day_total = source["day_total"];
	        this.day_complete = source["day_complete"];
	        this.sound_enabled = source["sound_enabled"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace main {
	
	export class SlotPatch {
	    task?: models.TaskRef;
	    label?: string;
	    clear_binding: boolean;
	    focus_minutes?: number;
	    break_minutes?: number;
	
	    static createFrom(source: any = {}) {
	        return new SlotPatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.task = this.convertValues(source["task"], models.TaskRef);
	        this.label = source["label"];
	        this.clear_binding = source["clear_binding"];
	        this.focus_minutes = source["focus_minutes"];
	        this.break_minutes = source["break_minutes"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace models {
	
	export class OverlayConfig {
	    size: number;
	    digits_size: number;
	    circle_opacity: number;
	    digits_opacity: number;
	    buttons_opacity: number;
	    show_time: boolean;
	    pos_x?: number;
	    pos_y?: number;
	
	    static createFrom(source: any = {}) {
	        return new OverlayConfig(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.size = source["size"];
	        this.digits_size = source["digits_size"];
	        this.circle_opacity = source["circle_opacity"];
	        this.digits_opacity = source["digits_opacity"];
	        this.buttons_opacity = source["buttons_opacity"];
	        this.show_time = source["show_time"];
	        this.pos_x = source["pos_x"];
	        this.pos_y = source["pos_y"];
	    }
	}
	export class PresetSlot {
	    focus_minutes: number;
	    break_minutes: number;
	
	    static createFrom(source: any = {}) {
	        return new PresetSlot(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.focus_minutes = source["focus_minutes"];
	        this.break_minutes = source["break_minutes"];
	    }
	}
	export class Preset {
	    id: number[];
	    name: string;
	    slots: PresetSlot[];
	    // Go type: time
	    created_at: any;
	    // Go type: time
	    updated_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Preset(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.slots = this.convertValues(source["slots"], PresetSlot);
	        this.created_at = this.convertValues(source["created_at"], null);
	        this.updated_at = this.convertValues(source["updated_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	
	export class Session {
	    id: number[];
	    kind: string;
	    // Go type: time
	    started_at: any;
	    // Go type: time
	    ended_at?: any;
	    planned_duration_seconds: number;
	    outcome?: string;
	    // Go type: time
	    paused_at?: any;
	    paused_total_seconds: number;
	    label?: string;
	    task_source?: string;
	    task_external_id?: string;
	    task_title_snapshot?: string;
	    // Go type: time
	    relabeled_at?: any;
	    // Go type: time
	    created_at: any;
	
	    static createFrom(source: any = {}) {
	        return new Session(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.kind = source["kind"];
	        this.started_at = this.convertValues(source["started_at"], null);
	        this.ended_at = this.convertValues(source["ended_at"], null);
	        this.planned_duration_seconds = source["planned_duration_seconds"];
	        this.outcome = source["outcome"];
	        this.paused_at = this.convertValues(source["paused_at"], null);
	        this.paused_total_seconds = source["paused_total_seconds"];
	        this.label = source["label"];
	        this.task_source = source["task_source"];
	        this.task_external_id = source["task_external_id"];
	        this.task_title_snapshot = source["task_title_snapshot"];
	        this.relabeled_at = this.convertValues(source["relabeled_at"], null);
	        this.created_at = this.convertValues(source["created_at"], null);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class Settings {
	    focus_duration_seconds: number;
	    short_break_seconds: number;
	    long_break_seconds: number;
	    day_blocks: number[];
	    study_before_work_minutes: number;
	    study_after_work_minutes: number;
	    work_share_percent: number;
	    auto_start_break: boolean;
	    auto_start_focus: boolean;
	    sound_enabled: boolean;
	    sound_file?: string;
	    overlay: OverlayConfig;
	
	    static createFrom(source: any = {}) {
	        return new Settings(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.focus_duration_seconds = source["focus_duration_seconds"];
	        this.short_break_seconds = source["short_break_seconds"];
	        this.long_break_seconds = source["long_break_seconds"];
	        this.day_blocks = source["day_blocks"];
	        this.study_before_work_minutes = source["study_before_work_minutes"];
	        this.study_after_work_minutes = source["study_after_work_minutes"];
	        this.work_share_percent = source["work_share_percent"];
	        this.auto_start_break = source["auto_start_break"];
	        this.auto_start_focus = source["auto_start_focus"];
	        this.sound_enabled = source["sound_enabled"];
	        this.sound_file = source["sound_file"];
	        this.overlay = this.convertValues(source["overlay"], OverlayConfig);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class TaskRef {
	    source: string;
	    external_id: string;
	    title_snapshot: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskRef(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.external_id = source["external_id"];
	        this.title_snapshot = source["title_snapshot"];
	    }
	}

}

export namespace plan {
	
	export class PickerNode {
	    kind: string;
	    id: string;
	    name: string;
	    source: string;
	    bind_task_id: string;
	    today: boolean;
	    children: PickerNode[];
	
	    static createFrom(source: any = {}) {
	        return new PickerNode(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.kind = source["kind"];
	        this.id = source["id"];
	        this.name = source["name"];
	        this.source = source["source"];
	        this.bind_task_id = source["bind_task_id"];
	        this.today = source["today"];
	        this.children = this.convertValues(source["children"], PickerNode);
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}
	export class ScheduleEntry {
	    weekday: number;
	    preset_name: string;
	
	    static createFrom(source: any = {}) {
	        return new ScheduleEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.weekday = source["weekday"];
	        this.preset_name = source["preset_name"];
	    }
	}
	export class SlotView {
	    idx: number;
	    task?: models.TaskRef;
	    label?: string;
	    focus_minutes?: number;
	    break_minutes?: number;
	    pinned: boolean;
	    done: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SlotView(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.idx = source["idx"];
	        this.task = this.convertValues(source["task"], models.TaskRef);
	        this.label = source["label"];
	        this.focus_minutes = source["focus_minutes"];
	        this.break_minutes = source["break_minutes"];
	        this.pinned = source["pinned"];
	        this.done = source["done"];
	    }
	
		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

}

export namespace tasksclient {
	
	export class TaskOption {
	    source: string;
	    external_id: string;
	    title: string;
	    topic_path: string;
	
	    static createFrom(source: any = {}) {
	        return new TaskOption(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.source = source["source"];
	        this.external_id = source["external_id"];
	        this.title = source["title"];
	        this.topic_path = source["topic_path"];
	    }
	}

}

