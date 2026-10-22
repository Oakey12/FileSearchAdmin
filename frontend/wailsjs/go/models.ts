export namespace constandstruct {
	
	export class ApplicationMatch {
	    Source: string;
	    Owner: string;
	    Name: string;
	    Version: string;
	    Path: string;
	
	    static createFrom(source: any = {}) {
	        return new ApplicationMatch(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Source = source["Source"];
	        this.Owner = source["Owner"];
	        this.Name = source["Name"];
	        this.Version = source["Version"];
	        this.Path = source["Path"];
	    }
	}
	export class EmployeeProcess {
	    ServerName: string;
	    ServerIP: string;
	    Owner: string;
	    PIDs: number[];
	    Sessions: number[];
	
	    static createFrom(source: any = {}) {
	        return new EmployeeProcess(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ServerName = source["ServerName"];
	        this.ServerIP = source["ServerIP"];
	        this.Owner = source["Owner"];
	        this.PIDs = source["PIDs"];
	        this.Sessions = source["Sessions"];
	    }
	}
	export class InstalledProgram {
	    Name: string;
	    Version: string;
	    Publisher: string;
	    InstallDate: string;
	    Architecture: string;
	    Owner: string;
	    RegistryPath: string;
	    CanSilentUninstall: boolean;
	    UninstallDescription: string;
	
	    static createFrom(source: any = {}) {
	        return new InstalledProgram(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Version = source["Version"];
	        this.Publisher = source["Publisher"];
	        this.InstallDate = source["InstallDate"];
	        this.Architecture = source["Architecture"];
	        this.Owner = source["Owner"];
	        this.RegistryPath = source["RegistryPath"];
	        this.CanSilentUninstall = source["CanSilentUninstall"];
	        this.UninstallDescription = source["UninstallDescription"];
	    }
	}
	export class MessageTemplate {
	    Name: string;
	    Text: string;
	
	    static createFrom(source: any = {}) {
	        return new MessageTemplate(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.Text = source["Text"];
	    }
	}
	export class RemoteFile {
	    Name: string;
	    FullPath: string;
	    Extension: string;
	    Size: number;
	    LastWriteTimeUTC: string;
	    IsText: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Name = source["Name"];
	        this.FullPath = source["FullPath"];
	        this.Extension = source["Extension"];
	        this.Size = source["Size"];
	        this.LastWriteTimeUTC = source["LastWriteTimeUTC"];
	        this.IsText = source["IsText"];
	    }
	}
	export class RemoteFolderResult {
	    Files: RemoteFile[];
	    Truncated: boolean;
	
	    static createFrom(source: any = {}) {
	        return new RemoteFolderResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.Files = this.convertValues(source["Files"], RemoteFile);
	        this.Truncated = source["Truncated"];
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
	export class RemoteSaveResult {
	    File: RemoteFile;
	    BackupPath: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteSaveResult(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.File = this.convertValues(source["File"], RemoteFile);
	        this.BackupPath = source["BackupPath"];
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
	export class RemoteTextDocument {
	    File: RemoteFile;
	    Content: string;
	    Encoding: string;
	    OriginalSHA256: string;
	
	    static createFrom(source: any = {}) {
	        return new RemoteTextDocument(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.File = this.convertValues(source["File"], RemoteFile);
	        this.Content = source["Content"];
	        this.Encoding = source["Encoding"];
	        this.OriginalSHA256 = source["OriginalSHA256"];
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
	export class RemoteUser {
	    DisplayName: string;
	    Username: string;
	    Domain: string;
	    SID: string;
	    ProfilePath: string;
	    SessionID: number;
	
	    static createFrom(source: any = {}) {
	        return new RemoteUser(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.DisplayName = source["DisplayName"];
	        this.Username = source["Username"];
	        this.Domain = source["Domain"];
	        this.SID = source["SID"];
	        this.ProfilePath = source["ProfilePath"];
	        this.SessionID = source["SessionID"];
	    }
	}
	export class Server {
	    name: string;
	    ip: string;
	
	    static createFrom(source: any = {}) {
	        return new Server(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.ip = source["ip"];
	    }
	}
	export class SystemInfo {
	    ComputerName: string;
	    Domain: string;
	    PartOfDomain: boolean;
	    LoggedOnUser: string;
	    Manufacturer: string;
	    Model: string;
	    SerialNumber: string;
	    OSCaption: string;
	    OSVersion: string;
	    OSBuild: string;
	    OSArchitecture: string;
	    OSInstallDate: string;
	    LastBootTime: string;
	    Uptime: string;
	    ProcessorName: string;
	    ProcessorCores: number;
	    ProcessorLogical: number;
	    ProcessorClockMHz: number;
	    MemoryTotalGB: string;
	    MemoryFreeGB: string;
	    IPAddresses: string[];
	    Disks: string[];
	    UserFolders: string[];
	
	    static createFrom(source: any = {}) {
	        return new SystemInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.ComputerName = source["ComputerName"];
	        this.Domain = source["Domain"];
	        this.PartOfDomain = source["PartOfDomain"];
	        this.LoggedOnUser = source["LoggedOnUser"];
	        this.Manufacturer = source["Manufacturer"];
	        this.Model = source["Model"];
	        this.SerialNumber = source["SerialNumber"];
	        this.OSCaption = source["OSCaption"];
	        this.OSVersion = source["OSVersion"];
	        this.OSBuild = source["OSBuild"];
	        this.OSArchitecture = source["OSArchitecture"];
	        this.OSInstallDate = source["OSInstallDate"];
	        this.LastBootTime = source["LastBootTime"];
	        this.Uptime = source["Uptime"];
	        this.ProcessorName = source["ProcessorName"];
	        this.ProcessorCores = source["ProcessorCores"];
	        this.ProcessorLogical = source["ProcessorLogical"];
	        this.ProcessorClockMHz = source["ProcessorClockMHz"];
	        this.MemoryTotalGB = source["MemoryTotalGB"];
	        this.MemoryFreeGB = source["MemoryFreeGB"];
	        this.IPAddresses = source["IPAddresses"];
	        this.Disks = source["Disks"];
	        this.UserFolders = source["UserFolders"];
	    }
	}

}

export namespace main {
	
	export class ScriptFile {
	    name: string;
	    text: string;
	
	    static createFrom(source: any = {}) {
	        return new ScriptFile(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.name = source["name"];
	        this.text = source["text"];
	    }
	}
	export class UpdateInfo {
	    available: boolean;
	    current: string;
	    version?: string;
	    notes?: string;
	
	    static createFrom(source: any = {}) {
	        return new UpdateInfo(source);
	    }
	
	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.available = source["available"];
	        this.current = source["current"];
	        this.version = source["version"];
	        this.notes = source["notes"];
	    }
	}

}

