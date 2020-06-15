import os
import argparse
import subprocess
import re
import shutil
import yaml
import time
import warnings
from datetime import datetime
import logging
warnings.filterwarnings("ignore")
encoding = 'utf-8'

def getLogFolderName(args):
    return args.helmchart + str(datetime.now().strftime("%Y-%m-%d-%H%M%S"))

def pvHelper(args) : 
    # Take details of PV and PVC from the amko pod helm chart
    helmResult = subprocess.check_output("helm get all %s -n %s" %(args.helmchart,args.namespace) , shell=True)
    logging.info("helm get all %s -n %s" %(args.helmchart,args.namespace))
    helmResult = helmResult.decode(encoding)
    return helmResult

def findPVCName(helmResult):
    start = helmResult.find("persistentVolumeClaim") + len("persistentVolumeClaim:")
    end = helmResult.find("\n", start)
    pvcName = helmResult[start:end].strip().strip("\"")
    if len(pvcName) > 0:
        return pvcName
    else:
        logging.info("Persistent Volume for pod is not defined\nReading logs directly from the pod")
        folderName = getLogFolderName(args)
        logging.info("Creating directory %s" %folderName)
        subprocess.check_output("mkdir %s" %folderName, shell=True)
        logging.info("kubectl logs %s -n %s --since %s > %s/amko.log" %(findPodName(args),args.namespace,args.since,folderName))
        subprocess.check_output("kubectl logs %s -n %s --since %s > %s/amko.log" %(findPodName(args),args.namespace,args.since,folderName) , shell=True)
        getCRD(args, folderName)
        logging.info("Zipping directory %s" %folderName)
        shutil.make_archive(folderName, 'zip', folderName)
        logging.info("Clean up: rm -r %s" %folderName)
        subprocess.check_output("rm -r %s" %folderName, shell=True)
        print("\nSuccess, Logs zipped into %s.zip\n" %folderName)
        return "no pvc"

def findPVMount(helmResult):
    start = helmResult.find("mountPath") + len("mountPath:")
    end = helmResult.find("\n", start)
    pvcMount = helmResult[start:end].strip()
    if len(pvcMount) > 0 :
        return pvcMount
    else:
        print("Persistent Volume Mount for pod is not defined\nMounting the log files to /log path\n")
        return "/log"

def findLogFileName(helmResult):
    start = helmResult.find("logFile") + len("logFile:")
    end = helmResult.find("\n", start)
    pvcMount = helmResult[start:end].strip()
    if len(pvcMount) > 0 :
        return pvcMount
    else:
        return "amko.log"

def editDeploymentFile(pvcName,pvMount,args):
    deploymentDict = {'apiVersion': 'v1', 'kind':'Pod', 'metadata':{'name': 'custom-backup-pod', 'namespace': '' }, 'spec':{'containers':[{'image': 'avinetworks/server-os', 'name': 'myfrontend', 'volumeMounts':[{'mountPath': '', 'name': 'mypd'}]}], 'volumes':[{'name': 'mypd', 'persistentVolumeClaim':{'claimName': ''}}]}} 
    deploymentDict['spec']['containers'][0]['volumeMounts'][0]['mountPath'] = pvMount
    deploymentDict['spec']['volumes'][0]['persistentVolumeClaim']['claimName'] = pvcName
    deploymentDict['metadata']['namespace'] = args.namespace
    pod = open('pod.yaml','w+')
    yaml.dump(deploymentDict, pod)

def findPodName(args):
    logging.info("kubectl get pod -n %s -l app.kubernetes.io/instance=%s" %(args.namespace, args.helmchart))
    Pods = subprocess.check_output("kubectl get pod -n %s -l app.kubernetes.io/instance=%s" %(args.namespace, args.helmchart) , shell=True)
    Pods = Pods.decode(encoding)
    allPods = Pods.splitlines()[1:]
    for podLine in allPods:
        podName = podLine.split(' ')[0]
        if podName.find("amko") is -1:
            continue 
        return podName
    return "not found"

def getGdp(args, folderName):
    logging.info("kubectl get gdp -n %s -o yaml > %s/gdp.yaml" %(args.namespace,folderName))
    subprocess.check_output("kubectl get gdp -n %s -o yaml > %s/gdp.yaml" %(args.namespace,folderName), shell=True)

def getGslb(args, folderName):
    logging.info("kubectl get gslbconfig -n %s -o yaml > %s/gslb.yaml" %(args.namespace,folderName))
    subprocess.check_output("kubectl get gslbconfig -n %s -o yaml > %s/gslb.yaml" %(args.namespace,folderName), shell=True)

def getCRD(args, folderName):
    getGdp(args, folderName)
    getGslb(args, folderName)

def zipLogFile (args):
    podName = findPodName(args)
    if podName == "not found":
        print("\nNo amko pod in the specified helm chart\n")
        return 0
    try:
        #Find details of the amko pod
        logging.info("kubectl describe pod %s -n %s" %(podName,args.namespace))
        statusOfAmkoPod =  subprocess.check_output("kubectl describe pod %s -n %s" %(podName,args.namespace) , shell=True)
        statusOfAmkoPod =  statusOfAmkoPod.decode(encoding)
    except:
        #If details couldnt be fetched, the kubectl describe raises any exception, then return failure
        print("Error fetching out the describe details of amko pod\n")
        return 0

    helmResult = pvHelper(args)
    pvcName = findPVCName(helmResult)
    if pvcName == "no pvc":
        return 1
    logging.info("PVC name is %s" %pvcName)
    pvMount = findPVMount(helmResult)
    logging.info("Logs are mounted in %s" %pvMount)
    logFileName = findLogFileName(helmResult)
    logging.info("Log file name is %s" %logFileName)
    folderName = getLogFolderName(args)

    #Check if the amko pod is up and running
    if (re.findall("Status: *Running", statusOfAmkoPod) and (re.findall("Restart Count: *0", statusOfAmkoPod))):
        #If amko pod is running, copy the log file to zip it
        try:
            logging.info("Creating directory %s" %folderName)
            subprocess.check_output("mkdir %s" %folderName, shell=True)
            logging.info("kubectl cp %s/%s:%s/%s %s/amko.log" %(args.namespace,podName,pvMount[1:],logFileName,folderName))
            subprocess.check_output("kubectl cp %s/%s:%s/%s %s/amko.log" %(args.namespace,podName,pvMount[1:],logFileName,folderName), shell=True)
            getCRD(args, folderName)
        except:
            print("Error in cp of amko pod\n")
            return 0
        logging.info("Zipping directory %s" %folderName)
        shutil.make_archive(folderName, 'zip', folderName)
        logging.info("Clean up: rm -r %s" %folderName)
        subprocess.check_output("rm -r %s" %folderName, shell=True)
        print("\nSuccess, Logs zipped into %s.zip\n" %folderName)
        return 1
    #If amko pod isnt running, then create backup pod named "mypod"
    else:
        #Creation of "mypod"
        logging.info("Creating backup pod as amko pod isn't running")
        editDeploymentFile(pvcName,pvMount,args)
        try:
            logging.info("kubectl apply -f pod.yaml")
            subprocess.check_output("kubectl apply -f pod.yaml", shell=True)
        except:
            return 0
        timeout = time.time() + 10
        #Wait for "mypod" to start running
        while(1):
            try:
                logging.info("kubectl describe pod custom-backup-pod -n %s" %args.namespace)
                statusOfBackupPod =  subprocess.check_output("kubectl describe pod custom-backup-pod -n %s" %args.namespace , shell=True)
                statusOfBackupPod = statusOfBackupPod.decode(encoding)
            except: 
                return 0
            if (re.findall("Status: *Running", statusOfBackupPod)):
                #Once "mypod" is running, copy the log file to zip it
                print("\nBackup pod \'custom-backup-pod\' started\n")
                logging.info("Creating directory %s" %folderName)
                subprocess.check_output("mkdir %s" %folderName, shell=True)
                logging.info("kubectl cp %s/custom-backup-pod:%s/%s %s/amko.log" %(args.namespace,pvMount[1:],logFileName,folderName))
                subprocess.check_output("kubectl cp %s/custom-backup-pod:%s/%s %s/amko.log" %(args.namespace,pvMount[1:], logFileName,folderName),shell=True)
                getCRD(args, folderName)
                logging.info("Zipping directory %s" %folderName)
                shutil.make_archive(folderName, 'zip', folderName)
                #Clean up
                logging.info("Clean up: kubectl delete pod custom-backup-pod -n %s" %args.namespace)
                subprocess.check_output("kubectl delete pod custom-backup-pod -n %s" %args.namespace , shell=True)
                logging.info("Clean up: rm pod.yaml")
                subprocess.check_output("rm pod.yaml", shell= True)
                logging.info("Clean up: rm -r %s" %folderName)
                subprocess.check_output("rm -r %s" %folderName, shell=True)

                print("\nSuccess, Logs zipped into %s.zip\n" %folderName)
                return 1
            time.sleep(2)
            if time.time()>timeout:
                break
        print("Couldn't create backup pod\n")
    return 0

if __name__ == "__main__":
    #Parsing cli arguments
    parser = argparse.ArgumentParser(formatter_class=argparse.RawTextHelpFormatter)
    parser.add_argument('-n', '--namespace', help='Namespace in which the amko pod is present' )
    parser.add_argument('-H', '--helmchart', help='Helm Chart name' )
    parser.add_argument('-w', '--wait', default= 10, help='Number of seconds to wait for the backup pod to start running before exiting\nDefault is 10 seconds' )
    parser.add_argument('-s', '--since',default='24h', help='For pods not having persistent volume storage the logs since a given time duration can be fetched.\nExample : mention the time as 2s(for 2 seconds) or 4m(for 4 mins) or 24h(for 24 hours)\nDefault is taken to be 24h' )
    args = parser.parse_args()

    logging.basicConfig(format='%(asctime)s - %(message)s', level=logging.INFO)

    if (not args.helmchart or not args.namespace):
        print("Scripts requires arguments\nTry \'python3 get_logs.py --help\' for more info\n\n")
        exit()

    if(zipLogFile(args)==0):
        print("Error getting log file\n\n")

