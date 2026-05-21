package lmsgateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type Mapper struct {
	courseMap     map[string]string
	assignmentMap map[string]string
}

func NewMapper(courseMapJSON string, assignmentMapJSON string) (Mapper, error) {
	courseMap, err := decodeMap(courseMapJSON, "course map")
	if err != nil {
		return Mapper{}, err
	}
	assignmentMap, err := decodeMap(assignmentMapJSON, "assignment map")
	if err != nil {
		return Mapper{}, err
	}
	return Mapper{courseMap: courseMap, assignmentMap: assignmentMap}, nil
}

func (m Mapper) Map(req LaunchRequest) (Mapping, error) {
	if err := validateLaunchRequest(req); err != nil {
		return Mapping{}, err
	}
	courseID := strings.TrimSpace(req.MoodleCourseID)
	if mapped := strings.TrimSpace(m.courseMap[courseID]); mapped != "" {
		courseID = mapped
	}
	labID := strings.TrimSpace(req.LabID)
	if labID == "" {
		labID = strings.TrimSpace(req.MoodleAssignmentID)
	}
	if mapped := strings.TrimSpace(m.assignmentMap[strings.TrimSpace(req.MoodleAssignmentID)]); mapped != "" {
		labID = mapped
	}
	return Mapping{
		StudentID: "moodle:" + strings.TrimSpace(req.MoodleUserID),
		CourseID:  courseID,
		LabID:     labID,
	}, nil
}

func (m Mapper) Description() MappingDescription {
	return MappingDescription{
		StudentIDRule:      "student_id = moodle:{moodle_user_id}",
		CourseIDRule:       "course_id = LMS_COURSE_MAP_JSON[moodle_course_id] or moodle_course_id",
		LabIDRule:          "lab_id = explicit lab_id, LMS_ASSIGNMENT_MAP_JSON[moodle_assignment_id], or moodle_assignment_id",
		ConfiguredCourses:  cloneMap(m.courseMap),
		ConfiguredLabs:     cloneMap(m.assignmentMap),
		LaunchEndpoint:     "POST /lms/moodle/launch",
		ResultEndpoint:     "GET /lms/moodle/launch/{launch_id}/result",
		SignatureAlgorithm: "X-LMS-Signature = hex(HMAC-SHA256(secret, X-LMS-Timestamp + '.' + raw_body))",
	}
}

func validateLaunchRequest(req LaunchRequest) error {
	if strings.TrimSpace(req.MoodleUserID) == "" {
		return errors.New("moodle_user_id is required")
	}
	if strings.TrimSpace(req.MoodleCourseID) == "" {
		return errors.New("moodle_course_id is required")
	}
	if strings.TrimSpace(req.MoodleAssignmentID) == "" {
		return errors.New("moodle_assignment_id is required")
	}
	return nil
}

func decodeMap(raw string, name string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	var result map[string]string
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	for key, value := range result {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			return nil, fmt.Errorf("%s contains empty key or value", name)
		}
		if trimmedKey != key {
			delete(result, key)
			result[trimmedKey] = trimmedValue
		} else {
			result[key] = trimmedValue
		}
	}
	return result, nil
}

func cloneMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}
