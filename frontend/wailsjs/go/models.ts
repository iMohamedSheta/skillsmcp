export namespace archive {
	
	export class ProjectMeta {
	    name: string;
	    slug: string;
	    description: string;
	    color: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectMeta(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.description = source["description"];
	        this.color = source["color"];
	    }
	}
	export class SkillEntry {
	    name: string;
	    description: string;
	    content: string;
	    category?: string;
	    tags?: string;
	    enabled: boolean;
	    sortOrder?: number;
	
	    static createFrom(source: any = {}) {
	        return new SkillEntry(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.tags = source["tags"];
	        this.enabled = source["enabled"];
	        this.sortOrder = source["sortOrder"];
	    }
	}

}

export namespace gitsync {
	
	export class WorkspaceFile {
	    name: string;
	    slug: string;
	    description?: string;
	    color?: string;
	    gitRemote?: string;
	    gitBranch?: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.gitRemote = source["gitRemote"];
	        this.gitBranch = source["gitBranch"];
	    }
	}
	export class ProjectConflict {
	    slug: string;
	    kind: string;
	    changedFields: string[];
	    local?: archive.ProjectMeta;
	    remote?: archive.ProjectMeta;
	
	    static createFrom(source: any = {}) {
	        return new ProjectConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slug = source["slug"];
	        this.kind = source["kind"];
	        this.changedFields = source["changedFields"];
	        this.local = this.convertValues(source["local"], archive.ProjectMeta);
	        this.remote = this.convertValues(source["remote"], archive.ProjectMeta);
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
	export class FileChange {
	    path: string;
	    status: string;
	    kind: string;
	    skillName?: string;
	    scope?: string;
	    projectSlug?: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new FileChange(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.path = source["path"];
	        this.status = source["status"];
	        this.kind = source["kind"];
	        this.skillName = source["skillName"];
	        this.scope = source["scope"];
	        this.projectSlug = source["projectSlug"];
	        this.detail = source["detail"];
	    }
	}
	export class SkillConflict {
	    name: string;
	    scope: string;
	    projectSlug: string;
	    kind: string;
	    changedFields: string[];
	    local?: archive.SkillEntry;
	    remote?: archive.SkillEntry;
	    localFile: string;
	    fileStatus: string;
	    detail: string;
	
	    static createFrom(source: any = {}) {
	        return new SkillConflict(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.scope = source["scope"];
	        this.projectSlug = source["projectSlug"];
	        this.kind = source["kind"];
	        this.changedFields = source["changedFields"];
	        this.local = this.convertValues(source["local"], archive.SkillEntry);
	        this.remote = this.convertValues(source["remote"], archive.SkillEntry);
	        this.localFile = source["localFile"];
	        this.fileStatus = source["fileStatus"];
	        this.detail = source["detail"];
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
	export class ConflictsResult {
	    workspace: string;
	    branch: string;
	    remote: string;
	    fetched: boolean;
	    hasConflicts: boolean;
	    localSkills: number;
	    remoteSkills: number;
	    addedLocal: number;
	    addedRemote: number;
	    modified: number;
	    unchanged: number;
	    skills: SkillConflict[];
	    files: FileChange[];
	    projects: ProjectConflict[];
	    workspaceLocal: WorkspaceFile;
	    workspaceRemote?: WorkspaceFile;
	    workspaceChangedFields: string[];
	    otherFiles: string[];
	    detail: string;
	    hint?: string;
	
	    static createFrom(source: any = {}) {
	        return new ConflictsResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.workspace = source["workspace"];
	        this.branch = source["branch"];
	        this.remote = source["remote"];
	        this.fetched = source["fetched"];
	        this.hasConflicts = source["hasConflicts"];
	        this.localSkills = source["localSkills"];
	        this.remoteSkills = source["remoteSkills"];
	        this.addedLocal = source["addedLocal"];
	        this.addedRemote = source["addedRemote"];
	        this.modified = source["modified"];
	        this.unchanged = source["unchanged"];
	        this.skills = this.convertValues(source["skills"], SkillConflict);
	        this.files = this.convertValues(source["files"], FileChange);
	        this.projects = this.convertValues(source["projects"], ProjectConflict);
	        this.workspaceLocal = this.convertValues(source["workspaceLocal"], WorkspaceFile);
	        this.workspaceRemote = this.convertValues(source["workspaceRemote"], WorkspaceFile);
	        this.workspaceChangedFields = source["workspaceChangedFields"];
	        this.otherFiles = source["otherFiles"];
	        this.detail = source["detail"];
	        this.hint = source["hint"];
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
	
	
	export class ProjectResolution {
	    slug: string;
	    action: string;
	    merged?: archive.ProjectMeta;
	
	    static createFrom(source: any = {}) {
	        return new ProjectResolution(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.slug = source["slug"];
	        this.action = source["action"];
	        this.merged = this.convertValues(source["merged"], archive.ProjectMeta);
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
	export class SkillResolution {
	    name: string;
	    scope: string;
	    projectSlug: string;
	    action: string;
	    merged?: archive.SkillEntry;
	
	    static createFrom(source: any = {}) {
	        return new SkillResolution(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.scope = source["scope"];
	        this.projectSlug = source["projectSlug"];
	        this.action = source["action"];
	        this.merged = this.convertValues(source["merged"], archive.SkillEntry);
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
	export class ResolveRequest {
	    skills: SkillResolution[];
	    projects: ProjectResolution[];
	    workspaceAction: string;
	    workspaceMerged?: WorkspaceFile;
	
	    static createFrom(source: any = {}) {
	        return new ResolveRequest(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.skills = this.convertValues(source["skills"], SkillResolution);
	        this.projects = this.convertValues(source["projects"], ProjectResolution);
	        this.workspaceAction = source["workspaceAction"];
	        this.workspaceMerged = this.convertValues(source["workspaceMerged"], WorkspaceFile);
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
	export class ResolveResult {
	    updated: number;
	    created: number;
	    deleted: number;
	    skipped: number;
	    projects: number;
	    detail: string;
	    filesNote: string;
	
	    static createFrom(source: any = {}) {
	        return new ResolveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.updated = source["updated"];
	        this.created = source["created"];
	        this.deleted = source["deleted"];
	        this.skipped = source["skipped"];
	        this.projects = source["projects"];
	        this.detail = source["detail"];
	        this.filesNote = source["filesNote"];
	    }
	}
	
	

}

export namespace model {
	
	export class Project {
	    id: string;
	    name: string;
	    slug: string;
	    description: string;
	    color: string;
	    workspaceId: string;
	    workspaceSlug: string;
	    workspaceName: string;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Project(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.workspaceId = source["workspaceId"];
	        this.workspaceSlug = source["workspaceSlug"];
	        this.workspaceName = source["workspaceName"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Skill {
	    id: string;
	    name: string;
	    description: string;
	    content: string;
	    category: string;
	    tags: string;
	    scope: string;
	    projectId: string;
	    projectSlug: string;
	    projectName: string;
	    workspaceId: string;
	    workspaceSlug: string;
	    workspaceName: string;
	    enabled: boolean;
	    sortOrder: number;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Skill(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.tags = source["tags"];
	        this.scope = source["scope"];
	        this.projectId = source["projectId"];
	        this.projectSlug = source["projectSlug"];
	        this.projectName = source["projectName"];
	        this.workspaceId = source["workspaceId"];
	        this.workspaceSlug = source["workspaceSlug"];
	        this.workspaceName = source["workspaceName"];
	        this.enabled = source["enabled"];
	        this.sortOrder = source["sortOrder"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}
	export class Workspace {
	    id: string;
	    name: string;
	    slug: string;
	    description: string;
	    color: string;
	    gitRemote: string;
	    gitBranch: string;
	    hasToken: boolean;
	    isMain: boolean;
	    createdAt: string;
	    updatedAt: string;
	
	    static createFrom(source: any = {}) {
	        return new Workspace(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.gitRemote = source["gitRemote"];
	        this.gitBranch = source["gitBranch"];
	        this.hasToken = source["hasToken"];
	        this.isMain = source["isMain"];
	        this.createdAt = source["createdAt"];
	        this.updatedAt = source["updatedAt"];
	    }
	}

}

export namespace store {
	
	export class ProjectInput {
	    name: string;
	    slug: string;
	    description: string;
	    color: string;
	    workspaceId: string;
	
	    static createFrom(source: any = {}) {
	        return new ProjectInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.workspaceId = source["workspaceId"];
	    }
	}
	export class SkillInput {
	    name: string;
	    description: string;
	    content: string;
	    category: string;
	    tags: string;
	    scope: string;
	    projectId: string;
	    workspaceId: string;
	    enabled: boolean;
	
	    static createFrom(source: any = {}) {
	        return new SkillInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.description = source["description"];
	        this.content = source["content"];
	        this.category = source["category"];
	        this.tags = source["tags"];
	        this.scope = source["scope"];
	        this.projectId = source["projectId"];
	        this.workspaceId = source["workspaceId"];
	        this.enabled = source["enabled"];
	    }
	}
	export class WorkspaceInput {
	    name: string;
	    slug: string;
	    description: string;
	    color: string;
	    gitRemote: string;
	    gitBranch: string;
	    gitToken: string;
	
	    static createFrom(source: any = {}) {
	        return new WorkspaceInput(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.slug = source["slug"];
	        this.description = source["description"];
	        this.color = source["color"];
	        this.gitRemote = source["gitRemote"];
	        this.gitBranch = source["gitBranch"];
	        this.gitToken = source["gitToken"];
	    }
	}

}

export namespace update {
	
	export class Info {
	    currentVersion: string;
	    latestVersion: string;
	    releaseName: string;
	    notes: string;
	    pageUrl: string;
	    assetName: string;
	    downloadUrl: string;
	    size: number;
	    publishedAt: string;
	    updateAvailable: boolean;
	    canInstall: boolean;
	    platform: string;
	
	    static createFrom(source: any = {}) {
	        return new Info(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.currentVersion = source["currentVersion"];
	        this.latestVersion = source["latestVersion"];
	        this.releaseName = source["releaseName"];
	        this.notes = source["notes"];
	        this.pageUrl = source["pageUrl"];
	        this.assetName = source["assetName"];
	        this.downloadUrl = source["downloadUrl"];
	        this.size = source["size"];
	        this.publishedAt = source["publishedAt"];
	        this.updateAvailable = source["updateAvailable"];
	        this.canInstall = source["canInstall"];
	        this.platform = source["platform"];
	    }
	}

}

