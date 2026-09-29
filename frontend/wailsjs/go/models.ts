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

